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
Get the name of the NATS credentials secret
*/}}
{{- define "nats-k8s-oidc-callout.natsSecretName" -}}
{{- if .Values.nats.credentials.create }}
{{- printf "%s-nats-creds" (include "nats-k8s-oidc-callout.fullname" .) }}
{{- else }}
{{- required "nats.credentials.existingSecret is required when nats.credentials.create=false" .Values.nats.credentials.existingSecret }}
{{- end }}
{{- end }}

{{/*
Get the key in the NATS credentials secret
*/}}
{{- define "nats-k8s-oidc-callout.natsSecretKey" -}}
{{- if .Values.nats.credentials.create }}
{{- print "credentials" }}
{{- else }}
{{- .Values.nats.credentials.existingSecretKey | default "credentials" }}
{{- end }}
{{- end }}
