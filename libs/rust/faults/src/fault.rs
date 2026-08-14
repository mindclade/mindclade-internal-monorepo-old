//! Fault value and retry metadata.

use crate::{Code, Context, ContextValue};
use std::error::Error;
use std::fmt;
use std::sync::Arc;
use std::time::Duration;

/// Retry guidance attached to a fault.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RetryHint {
    Never,
    Immediate,
    After(Duration),
}

impl RetryHint {
    /// Whether the caller may retry.
    #[must_use]
    pub const fn is_retryable(self) -> bool {
        !matches!(self, Self::Never)
    }
}

/// Standard result alias.
pub type FaultResult<T> = Result<T, Fault>;

/// Structured, cloneable error used by library and service boundaries.
#[derive(Clone, Debug)]
pub struct Fault {
    code: Code,
    message: String,
    context: Context,
    retry: RetryHint,
    source: Option<Arc<dyn Error + Send + Sync + 'static>>,
}

impl Fault {
    /// Creates a fault with retry behavior inferred from the code.
    #[must_use]
    pub fn new(code: Code, message: impl Into<String>) -> Self {
        let retry = if code.is_transient() {
            RetryHint::Immediate
        } else {
            RetryHint::Never
        };
        Self {
            code,
            message: message.into(),
            context: Context::new(),
            retry,
            source: None,
        }
    }
    /// Creates an invalid-argument fault.
    #[must_use]
    pub fn invalid_argument(message: impl Into<String>) -> Self {
        Self::new(Code::InvalidArgument, message)
    }
    /// Creates an internal fault.
    #[must_use]
    pub fn internal(message: impl Into<String>) -> Self {
        Self::new(Code::Internal, message)
    }
    /// Creates a data-loss fault.
    #[must_use]
    pub fn data_loss(message: impl Into<String>) -> Self {
        Self::new(Code::DataLoss, message)
    }
    /// Returns the stable error code.
    #[must_use]
    pub const fn code(&self) -> Code {
        self.code
    }
    /// Returns the human-readable, non-secret message.
    #[must_use]
    pub fn message(&self) -> &str {
        &self.message
    }
    /// Returns structured context.
    #[must_use]
    pub const fn context(&self) -> &Context {
        &self.context
    }
    /// Returns retry guidance.
    #[must_use]
    pub const fn retry_hint(&self) -> RetryHint {
        self.retry
    }
    /// Adds a non-sensitive context field.
    #[must_use]
    pub fn with_context(mut self, key: impl Into<String>, value: impl Into<ContextValue>) -> Self {
        self.context.insert(key, value);
        self
    }
    /// Records only that a sensitive context field existed.
    #[must_use]
    pub fn with_sensitive_context(mut self, key: impl Into<String>) -> Self {
        self.context.insert_sensitive(key);
        self
    }
    /// Overrides retry guidance.
    #[must_use]
    pub const fn with_retry_hint(mut self, retry: RetryHint) -> Self {
        self.retry = retry;
        self
    }
    /// Attaches a source error without exposing it in the default display.
    #[must_use]
    pub fn with_source<E>(mut self, source: E) -> Self
    where
        E: Error + Send + Sync + 'static,
    {
        self.source = Some(Arc::new(source));
        self
    }
}

impl fmt::Display for Fault {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{}: {}", self.code, self.message)?;
        if !self.context.is_empty() {
            formatter.write_str(" (")?;
            for (index, (key, value)) in self.context.iter().enumerate() {
                if index > 0 {
                    formatter.write_str(", ")?;
                }
                write!(formatter, "{key}={value}")?;
            }
            formatter.write_str(")")?;
        }
        Ok(())
    }
}

impl Error for Fault {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        self.source
            .as_deref()
            .map(|source| source as &(dyn Error + 'static))
    }
}
