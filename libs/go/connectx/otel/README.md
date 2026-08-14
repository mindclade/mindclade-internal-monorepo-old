# Connect OpenTelemetry adapter

Optional wrapper around `otelconnect.NewInterceptor`. On handlers, place the
interceptor in `interceptors.ServerConfig.Outer` so telemetry observes the
canonical Connect error produced by fault translation. On clients, pass it to
`interceptors.Client(...)`; client additions are inside fault translation and
therefore observe the wire status before it is reconstructed as a local fault.
