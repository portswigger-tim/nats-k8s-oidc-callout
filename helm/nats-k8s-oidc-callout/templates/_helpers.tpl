{{/*
Expand the name of the chart.
*/}}
{{- define "nats-k8s-oidc-callout.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nats-k8s-oidc-callout.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "nats-k8s-oidc-callout.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nats-k8s-oidc-callout.labels" -}}
helm.sh/chart: {{ include "nats-k8s-oidc-callout.chart" . }}
{{ include "nats-k8s-oidc-callout.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nats-k8s-oidc-callout.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nats-k8s-oidc-callout.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nats-k8s-oidc-callout.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "nats-k8s-oidc-callout.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the name of the NATS user credentials secret
*/}}
{{- define "nats-k8s-oidc-callout.natsUserCredsSecretName" -}}
{{- if .Values.nats.userCredentials.create }}
{{- printf "%s-nats-user-creds" (include "nats-k8s-oidc-callout.fullname" .) }}
{{- else }}
{{- .Values.nats.userCredentials.existingSecret }}
{{- end }}
{{- end }}

{{/*
Get the key in the NATS user credentials secret
*/}}
{{- define "nats-k8s-oidc-callout.natsUserCredsSecretKey" -}}
{{- if .Values.nats.userCredentials.create }}
{{- print "user.creds" }}
{{- else }}
{{- .Values.nats.userCredentials.existingSecretKey | default "user.creds" }}
{{- end }}
{{- end }}

{{/*
Get the name of the NATS signing key secret
*/}}
{{- define "nats-k8s-oidc-callout.natsSigningKeySecretName" -}}
{{- if .Values.nats.signingKey.create }}
{{- printf "%s-nats-signing-key" (include "nats-k8s-oidc-callout.fullname" .) }}
{{- else }}
{{- required "nats.signingKey.existingSecret is required when nats.signingKey.create=false" .Values.nats.signingKey.existingSecret }}
{{- end }}
{{- end }}

{{/*
Get the key in the NATS signing key secret
*/}}
{{- define "nats-k8s-oidc-callout.natsSigningKeySecretKey" -}}
{{- if .Values.nats.signingKey.create }}
{{- print "signing.key" }}
{{- else }}
{{- .Values.nats.signingKey.existingSecretKey | default "signing.key" }}
{{- end }}
{{- end }}
