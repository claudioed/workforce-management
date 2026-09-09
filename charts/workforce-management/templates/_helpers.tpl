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

{{/*
Name of the Secret holding the REST identity keys (API_READ_KEY,
API_READWRITE_KEY and the per-peer <PEER>_API_KEY bearers), when the chart
creates its own (ADR-0017 / fleet ADR 0005).
*/}}
{{- define "workforce-management.authSecretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- include "workforce-management.fullname" . }}-auth
{{- end }}
{{- end }}

{{/*
The REST identity env block shared by the OLTP and reports Deployments:
AUTH_MODE from the ConfigMap and the inbound bearer keys from the auth
Secret. Every secretKeyRef is optional so a release with no keys (auth off)
still schedules.
*/}}
{{- define "workforce-management.authEnv" -}}
- name: AUTH_MODE
  valueFrom:
    configMapKeyRef:
      name: {{ include "workforce-management.fullname" . }}
      key: AUTH_MODE
- name: API_READ_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "workforce-management.authSecretName" . }}
      key: API_READ_KEY
      optional: true
- name: API_READWRITE_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "workforce-management.authSecretName" . }}
      key: API_READWRITE_KEY
      optional: true
{{- end }}

{{/*
Fully qualified name of the analytics projector deployment (ADR-0010).
*/}}
{{- define "workforce-management.projectorFullname" -}}
{{- include "workforce-management.fullname" . }}-projector
{{- end }}

{{/*
Fully qualified name of the analytics reports deployment/service (ADR-0010).
*/}}
{{- define "workforce-management.reportsFullname" -}}
{{- include "workforce-management.fullname" . }}-reports
{{- end }}

{{/*
Name of the Secret holding the analytics DSNs, when the chart creates its own.
*/}}
{{- define "workforce-management.analyticsSecretName" -}}
{{- if .Values.analytics.database.existingSecret }}
{{- .Values.analytics.database.existingSecret }}
{{- else }}
{{- include "workforce-management.fullname" . }}-analytics
{{- end }}
{{- end }}

{{/*
Fully qualified name of the MCP server deployment/service (ADR-0008).
*/}}
{{- define "workforce-management.mcpFullname" -}}
{{- include "workforce-management.fullname" . }}-mcp
{{- end }}

{{/*
Name of the Secret holding the MCP bearer keys, when the chart creates its own.
*/}}
{{- define "workforce-management.mcpSecretName" -}}
{{- if .Values.mcp.existingSecret }}
{{- .Values.mcp.existingSecret }}
{{- else }}
{{- include "workforce-management.fullname" . }}-mcp
{{- end }}
{{- end }}
