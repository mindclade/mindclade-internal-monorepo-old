{{/* Copyright © 2026 Mindclade, LLC. All Rights Reserved. */}}
{{- define "mindclade-mlflow.name" -}}mlflow{{- end -}}

{{- define "mindclade-mlflow.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "mindclade-mlflow.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mindclade-mlflow.labels" -}}
app.kubernetes.io/name: {{ include "mindclade-mlflow.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: metadata-lineage
app.kubernetes.io/part-of: mindclade-platform
app.kubernetes.io/managed-by: {{ .Release.Service }}
mindclade.dev/authority: mirror
mindclade.dev/owner: ml-platform
{{- end -}}

{{- define "mindclade-mlflow.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mindclade-mlflow.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "mindclade-mlflow.activationGuard" -}}
{{- if eq .Values.activation.releaseEvidenceDigest "sha256:0000000000000000000000000000000000000000000000000000000000000000" -}}
{{- fail "activation requires a nonzero release evidence digest" -}}
{{- end -}}
{{- if eq .Values.auth.existingSecret "SET_BY_ACTIVATION_BUNDLE" -}}
{{- fail "activation requires auth.existingSecret" -}}
{{- end -}}
{{- if eq .Values.artifacts.destination "SET_BY_ACTIVATION_BUNDLE" -}}
{{- fail "activation requires artifacts.destination" -}}
{{- end -}}
{{- if eq .Values.artifacts.modelVersionSourceValidationRegex "SET_BY_ACTIVATION_BUNDLE" -}}
{{- fail "activation requires artifacts.modelVersionSourceValidationRegex" -}}
{{- end -}}
{{- if eq .Values.serviceAccount.gcpServiceAccount "SET_BY_ACTIVATION_BUNDLE" -}}
{{- fail "activation requires serviceAccount.gcpServiceAccount" -}}
{{- end -}}
{{- end -}}
