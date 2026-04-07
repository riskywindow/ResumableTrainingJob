{{/*
Expand the name of the chart.
*/}}
{{- define "rtj-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "rtj-operator.fullname" -}}
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
{{- define "rtj-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "rtj-operator.labels" -}}
helm.sh/chart: {{ include "rtj-operator.chart" . }}
{{ include "rtj-operator.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: checkpoint-native-preemption-controller
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "rtj-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "rtj-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "rtj-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "rtj-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Webhook service name.
*/}}
{{- define "rtj-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "rtj-operator.fullname" .) }}
{{- end }}

{{/*
Metrics service name.
*/}}
{{- define "rtj-operator.metricsServiceName" -}}
{{- printf "%s-metrics" (include "rtj-operator.fullname" .) }}
{{- end }}

{{/*
Webhook certificate name.
*/}}
{{- define "rtj-operator.webhookCertName" -}}
{{- printf "%s-webhook-cert" (include "rtj-operator.fullname" .) }}
{{- end }}

{{/*
Webhook certificate secret name.
*/}}
{{- define "rtj-operator.webhookCertSecretName" -}}
{{- printf "%s-webhook-tls" (include "rtj-operator.fullname" .) }}
{{- end }}

{{/*
Namespace to use.
*/}}
{{- define "rtj-operator.namespace" -}}
{{- default .Release.Namespace .Values.namespace }}
{{- end }}

{{/*
Image reference.
*/}}
{{- define "rtj-operator.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}
