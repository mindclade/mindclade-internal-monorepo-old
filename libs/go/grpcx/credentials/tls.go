// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package credentials

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	grpccredentials "google.golang.org/grpc/credentials"
	"go.mindclade.dev/libs/go/faults"
	"os"
	"strings"
)

const DefaultMinimumTLSVersion = uint16(tls.VersionTLS12)

type ServerConfig struct {
	Certificate              tls.Certificate
	ClientCAs                *x509.CertPool
	RequireClientCertificate bool
	MinimumVersion           uint16
	NextProtos               []string
}

func NewServer(config ServerConfig) (grpccredentials.TransportCredentials, error) {
	if len(config.Certificate.Certificate) == 0 {
		return nil, faults.New(faults.CodeInvalidArgument, "server TLS certificate is required", faults.WithReason("missing_server_certificate"))
	}
	if config.MinimumVersion == 0 {
		config.MinimumVersion = DefaultMinimumTLSVersion
	}
	if config.MinimumVersion != tls.VersionTLS12 && config.MinimumVersion != tls.VersionTLS13 {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid minimum TLS version", faults.WithReason("invalid_minimum_tls_version"))
	}
	tlsConfig := &tls.Config{MinVersion: config.MinimumVersion, Certificates: []tls.Certificate{cloneCertificate(config.Certificate)}, NextProtos: cloneStrings(config.NextProtos)}
	if config.RequireClientCertificate {
		if config.ClientCAs == nil {
			return nil, faults.New(faults.CodeInvalidArgument, "client certificate authorities are required", faults.WithReason("missing_client_ca"))
		}
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = config.ClientCAs.Clone()
	}
	return grpccredentials.NewTLS(tlsConfig), nil
}

type ClientConfig struct {
	RootCAs        *x509.CertPool
	ServerName     string
	Certificate    *tls.Certificate
	MinimumVersion uint16
	NextProtos     []string
}

func NewClient(config ClientConfig) (grpccredentials.TransportCredentials, error) {
	if config.RootCAs == nil {
		return nil, faults.New(faults.CodeInvalidArgument, "root certificate authorities are required", faults.WithReason("missing_root_ca"))
	}
	if strings.TrimSpace(config.ServerName) == "" {
		return nil, faults.New(faults.CodeInvalidArgument, "TLS server name is required", faults.WithReason("missing_tls_server_name"))
	}
	if config.MinimumVersion == 0 {
		config.MinimumVersion = DefaultMinimumTLSVersion
	}
	if config.MinimumVersion != tls.VersionTLS12 && config.MinimumVersion != tls.VersionTLS13 {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid minimum TLS version", faults.WithReason("invalid_minimum_tls_version"))
	}
	tlsConfig := &tls.Config{MinVersion: config.MinimumVersion, RootCAs: config.RootCAs.Clone(), ServerName: strings.TrimSpace(config.ServerName), NextProtos: cloneStrings(config.NextProtos)}
	if config.Certificate != nil {
		tlsConfig.Certificates = []tls.Certificate{cloneCertificate(*config.Certificate)}
	}
	return grpccredentials.NewTLS(tlsConfig), nil
}
func LoadCertificate(certificateFile, keyFile string) (tls.Certificate, error) {
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		return tls.Certificate{}, faults.Wrap(err, faults.CodeInvalidArgument, "unable to load TLS certificate", faults.WithReason("tls_certificate_load_failed"))
	}
	return certificate, nil
}
func LoadCertPool(path string) (*x509.CertPool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, faults.Wrap(err, faults.CodeNotFound, "unable to load certificate authorities", faults.WithReason("ca_file_read_failed"))
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(contents) {
		return nil, faults.Wrap(errors.New("no certificates found"), faults.CodeInvalidArgument, "invalid certificate authorities", faults.WithReason("invalid_ca_bundle"))
	}
	return pool, nil
}
func cloneStrings(input []string) []string {
	if input == nil {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func cloneCertificate(input tls.Certificate) tls.Certificate {
	output := input
	output.Certificate = cloneByteSlices(input.Certificate)
	output.OCSPStaple = append([]byte(nil), input.OCSPStaple...)
	if input.SignedCertificateTimestamps != nil {
		output.SignedCertificateTimestamps = cloneByteSlices(input.SignedCertificateTimestamps)
	}
	return output
}

func cloneByteSlices(input [][]byte) [][]byte {
	if input == nil {
		return nil
	}
	output := make([][]byte, len(input))
	for index, value := range input {
		output[index] = append([]byte(nil), value...)
	}
	return output
}
