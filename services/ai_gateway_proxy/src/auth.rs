// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use futures_util::StreamExt;
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult};
use reqwest::{Client, Url};
use ring::signature::{self, RsaPublicKeyComponents};
use serde::Deserialize;
use std::{
    collections::BTreeMap,
    future::Future,
    pin::Pin,
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::sync::{Mutex, RwLock};

const MAXIMUM_TOKEN_BYTES: usize = 16 << 10;
const MAXIMUM_KEYS_BYTES: usize = 1 << 20;
const MAXIMUM_KEYS: usize = 64;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Identity {
    pub subject: String,
    pub email: Option<String>,
}

impl Identity {
    /// Returns the same non-PII issuer/subject key produced by the Go auth
    /// library for a Google principal. Policy and budget records use this key
    /// so the proxy never delegates an email address or raw Google subject.
    #[must_use]
    pub fn policy_subject(&self) -> String {
        let mut identity = b"https://accounts.google.com\0".to_vec();
        identity.extend_from_slice(self.subject.as_bytes());
        format!("google-{}", hash_bytes(&identity).to_hex())
    }
}

pub trait IdentityVerifier: Send + Sync + std::fmt::Debug {
    fn verify<'a>(
        &'a self,
        token: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Identity>> + Send + 'a>>;
}

#[derive(Clone, Debug)]
struct RsaKey {
    modulus: Vec<u8>,
    exponent: Vec<u8>,
}

#[derive(Debug, Default)]
struct KeyState {
    keys: BTreeMap<String, RsaKey>,
    fetched_at: u64,
    last_unknown_refresh: u64,
}

#[derive(Clone, Debug)]
pub struct GoogleIdTokenVerifier {
    audience: Arc<str>,
    jwks_url: Url,
    client: Client,
    state: Arc<RwLock<KeyState>>,
    refresh: Arc<Mutex<()>>,
}

#[derive(Debug, Deserialize)]
struct JwtHeader {
    alg: String,
    kid: String,
}

#[derive(Debug, Deserialize)]
struct Claims {
    iss: String,
    sub: String,
    aud: String,
    #[serde(default)]
    email: Option<String>,
    #[serde(default)]
    email_verified: Option<bool>,
    iat: i64,
    exp: i64,
}

#[derive(Debug, Deserialize)]
struct JwkSet {
    keys: Vec<Jwk>,
}

#[derive(Debug, Deserialize)]
struct Jwk {
    kid: String,
    kty: String,
    alg: String,
    n: String,
    e: String,
}

impl GoogleIdTokenVerifier {
    pub fn new(audience: String, jwks_url: Url, timeout: Duration) -> FaultResult<Self> {
        if audience.trim().is_empty() || audience.len() > 2048 {
            return Err(Fault::invalid_argument(
                "Google ID-token audience is invalid",
            ));
        }
        let client = Client::builder()
            .connect_timeout(timeout)
            .timeout(timeout)
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .map_err(|error| {
                Fault::new(
                    Code::FailedPrecondition,
                    "Google JWKS client configuration failed",
                )
                .with_source(error)
            })?;
        Ok(Self {
            audience: Arc::from(audience),
            jwks_url,
            client,
            state: Arc::new(RwLock::new(KeyState::default())),
            refresh: Arc::new(Mutex::new(())),
        })
    }

    async fn verify_token(&self, token: &str) -> FaultResult<Identity> {
        if token.is_empty() || token.len() > MAXIMUM_TOKEN_BYTES {
            return Err(unauthenticated("Google ID token is missing or oversized"));
        }
        let mut parts = token.split('.');
        let header_segment = parts.next().unwrap_or_default();
        let claims_segment = parts.next().unwrap_or_default();
        let signature_segment = parts.next().unwrap_or_default();
        if header_segment.is_empty()
            || claims_segment.is_empty()
            || signature_segment.is_empty()
            || parts.next().is_some()
        {
            return Err(unauthenticated("Google ID token is malformed"));
        }
        let header: JwtHeader = decode_segment(header_segment)?;
        if header.alg != "RS256" || header.kid.is_empty() || header.kid.len() > 256 {
            return Err(unauthenticated("Google ID token signing header is invalid"));
        }
        let key = self.key(&header.kid).await?;
        let signature_bytes = URL_SAFE_NO_PAD
            .decode(signature_segment)
            .map_err(|_| unauthenticated("Google ID token signature is malformed"))?;
        let signing_input = format!("{header_segment}.{claims_segment}");
        RsaPublicKeyComponents {
            n: &key.modulus,
            e: &key.exponent,
        }
        .verify(
            &signature::RSA_PKCS1_2048_8192_SHA256,
            signing_input.as_bytes(),
            &signature_bytes,
        )
        .map_err(|_| unauthenticated("Google ID token signature is invalid"))?;
        let claims: Claims = decode_segment(claims_segment)?;
        if claims.iss != "https://accounts.google.com" && claims.iss != "accounts.google.com" {
            return Err(unauthenticated("Google ID token issuer is invalid"));
        }
        if claims.aud != self.audience.as_ref() {
            return Err(unauthenticated("Google ID token audience is invalid"));
        }
        if claims.sub.is_empty()
            || claims.sub.len() > 512
            || claims.sub.bytes().any(|byte| byte <= 0x20 || byte == 0x7f)
        {
            return Err(unauthenticated("Google ID token subject is invalid"));
        }
        let now = unix_seconds()?;
        let now_i64 = i64::try_from(now)
            .map_err(|_| Fault::new(Code::OutOfRange, "system time exceeds i64"))?;
        if claims.iat <= 0
            || claims.iat > now_i64.saturating_add(30)
            || claims.exp <= claims.iat
            || now_i64 >= claims.exp
        {
            return Err(unauthenticated("Google ID token lifetime is invalid"));
        }
        let email = claims
            .email
            .filter(|value| claims.email_verified == Some(true) && value.len() <= 512);
        Ok(Identity {
            subject: claims.sub,
            email,
        })
    }

    pub async fn prewarm(&self) -> FaultResult<()> {
        self.refresh_keys(unix_seconds()?).await
    }

    async fn key(&self, kid: &str) -> FaultResult<RsaKey> {
        let now = unix_seconds()?;
        {
            let state = self.state.read().await;
            if now.saturating_sub(state.fetched_at) < 3600 {
                if let Some(key) = state.keys.get(kid) {
                    return Ok(key.clone());
                }
                if now.saturating_sub(state.last_unknown_refresh) < 60 {
                    return Err(unauthenticated(
                        "Google ID token names an unknown signing key",
                    ));
                }
            }
        }
        self.refresh_keys(now).await?;
        self.state
            .read()
            .await
            .keys
            .get(kid)
            .cloned()
            .ok_or_else(|| unauthenticated("Google ID token names an unknown signing key"))
    }

    async fn refresh_keys(&self, now: u64) -> FaultResult<()> {
        let _guard = self.refresh.lock().await;
        {
            let state = self.state.read().await;
            if now.saturating_sub(state.fetched_at) < 60 && !state.keys.is_empty() {
                return Ok(());
            }
        }
        {
            let mut state = self.state.write().await;
            state.last_unknown_refresh = now;
        }
        let response = self
            .client
            .get(self.jwks_url.clone())
            .send()
            .await
            .map_err(|error| {
                Fault::new(Code::Unavailable, "Google signing keys are unavailable")
                    .with_source(error)
            })?;
        if !response.status().is_success()
            || response
                .content_length()
                .is_some_and(|length| length > MAXIMUM_KEYS_BYTES as u64)
        {
            return Err(Fault::new(
                Code::Unavailable,
                "Google signing keys response is invalid",
            ));
        }
        let mut stream = response.bytes_stream();
        let mut bytes = Vec::new();
        while let Some(chunk) = stream.next().await {
            let chunk = chunk.map_err(|error| {
                Fault::new(Code::Unavailable, "Google signing keys could not be read")
                    .with_source(error)
            })?;
            let next = bytes
                .len()
                .checked_add(chunk.len())
                .ok_or_else(|| Fault::new(Code::OutOfRange, "Google signing keys size overflow"))?;
            if next > MAXIMUM_KEYS_BYTES {
                return Err(Fault::new(
                    Code::Unavailable,
                    "Google signing keys response is oversized",
                ));
            }
            bytes.extend_from_slice(&chunk);
        }
        let jwks: JwkSet = serde_json::from_slice(&bytes).map_err(|error| {
            Fault::new(
                Code::Unavailable,
                "Google signing keys response is malformed",
            )
            .with_source(error)
        })?;
        if jwks.keys.is_empty() || jwks.keys.len() > MAXIMUM_KEYS {
            return Err(Fault::new(
                Code::Unavailable,
                "Google signing key count is invalid",
            ));
        }
        let mut keys = BTreeMap::new();
        for jwk in jwks.keys {
            if jwk.kty != "RSA" || jwk.alg != "RS256" || jwk.kid.is_empty() || jwk.kid.len() > 256 {
                continue;
            }
            let modulus = URL_SAFE_NO_PAD.decode(jwk.n).ok();
            let exponent = URL_SAFE_NO_PAD.decode(jwk.e).ok();
            let (Some(modulus), Some(exponent)) = (modulus, exponent) else {
                continue;
            };
            if modulus.len() < 256
                || exponent.is_empty()
                || exponent.len() > 8
                || keys.insert(jwk.kid, RsaKey { modulus, exponent }).is_some()
            {
                return Err(Fault::new(
                    Code::Unavailable,
                    "Google signing key set contains invalid or duplicate keys",
                ));
            }
        }
        if keys.is_empty() {
            return Err(Fault::new(
                Code::Unavailable,
                "Google signing key set contains no usable keys",
            ));
        }
        let mut state = self.state.write().await;
        state.keys = keys;
        state.fetched_at = now;
        Ok(())
    }
}

impl IdentityVerifier for GoogleIdTokenVerifier {
    fn verify<'a>(
        &'a self,
        token: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Identity>> + Send + 'a>> {
        Box::pin(self.verify_token(token))
    }
}

fn decode_segment<T: for<'de> Deserialize<'de>>(segment: &str) -> FaultResult<T> {
    let bytes = URL_SAFE_NO_PAD
        .decode(segment)
        .map_err(|_| unauthenticated("Google ID token segment is malformed"))?;
    if bytes.len() > MAXIMUM_TOKEN_BYTES {
        return Err(unauthenticated("Google ID token segment is oversized"));
    }
    serde_json::from_slice(&bytes)
        .map_err(|_| unauthenticated("Google ID token claims are malformed"))
}

fn unix_seconds() -> FaultResult<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .map_err(|error| {
            Fault::new(
                Code::FailedPrecondition,
                "system time is before the Unix epoch",
            )
            .with_source(error)
        })
}

fn unauthenticated(message: &str) -> Fault {
    Fault::new(Code::Unauthenticated, message)
}

#[cfg(test)]
mod tests {
    use super::Identity;

    #[test]
    fn policy_subject_matches_the_go_principal_key_contract() {
        let identity = Identity {
            subject: "service-account:caller".to_owned(),
            email: Some("caller@example.invalid".to_owned()),
        };
        assert_eq!(
            identity.policy_subject(),
            "google-ef8168663494fc8a1b3267fb3a9f929b155bd6a9fce5a83735c2d80f3830197c"
        );
    }
}
