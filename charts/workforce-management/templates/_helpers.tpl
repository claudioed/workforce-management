{{- define "workforce-management.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "workforce-management.fullname" -}}
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

{{- define "workforce-management.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "workforce-management.labels" -}}
helm.sh/chart: {{ include "workforce-management.chart" . }}
{{ include "workforce-management.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "workforce-management.selectorLabels" -}}
app.kubernetes.io/name: {{ include "workforce-management.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "workforce-management.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "workforce-management.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "workforce-management.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "workforce-management.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Fails chart rendering with a clear message if no DATABASE_URL source is
configured. This service crash-loops without one (requireEnv in main.go),
so we surface that as a helm install-time error instead.
*/}}
{{- define "workforce-management.requireDatabase" -}}
{{- if not (or .Values.database.url .Values.database.existingSecret) -}}
{{- fail "workforce-management requires database.url or database.existingSecret to be set — this service has no in-memory fallback and will crash-loop without DATABASE_URL." -}}
{{- end -}}
{{- end -}}
