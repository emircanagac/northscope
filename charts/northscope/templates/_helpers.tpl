{{- define "northscope.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "northscope.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "northscope.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "northscope.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "northscope.selectorLabels" -}}
app.kubernetes.io/name: {{ include "northscope.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "northscope.authSecretName" -}}
{{- if .Values.auth.existingSecret -}}
{{- .Values.auth.existingSecret -}}
{{- else -}}
{{- printf "%s-auth" (include "northscope.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Base64-encoded data of the chart-managed auth Secret. An explicit
auth.password wins; otherwise the password and session key already stored in
the release's Secret are reused, so upgrades keep credentials and sessions.
New values are generated only on first install. The result is computed once
per render so the Secret and the Deployment checksum always agree.
*/}}
{{- define "northscope.authSecretData" -}}
{{- if not (hasKey .Values.auth "__secretData") -}}
{{- $existing := lookup "v1" "Secret" .Release.Namespace (include "northscope.authSecretName" .) | default dict -}}
{{- $existingData := get $existing "data" | default dict -}}
{{- $password := "" -}}
{{- if .Values.auth.password -}}
{{- $password = .Values.auth.password | b64enc -}}
{{- else if get $existingData "password" -}}
{{- $password = get $existingData "password" -}}
{{- else -}}
{{- $password = randAlphaNum 24 | b64enc -}}
{{- end -}}
{{- $sessionKey := get $existingData "session-key" | default (randAlphaNum 64 | b64enc) -}}
{{- $_ := set .Values.auth "__secretData" (dict "username" (.Values.auth.username | b64enc) "password" $password "session-key" $sessionKey) -}}
{{- end -}}
{{- toYaml (get .Values.auth "__secretData") -}}
{{- end -}}

{{- define "northscope.validateAuth" -}}
{{- if .Values.auth.enabled -}}
{{- if not (has (toString .Values.auth.cookieSecure) (list "auto" "true" "false")) -}}
{{- fail "auth.cookieSecure must be auto, true, or false" -}}
{{- end -}}
{{- if and (not .Values.auth.existingSecret) (not .Values.auth.username) -}}
{{- fail "auth.username must not be empty" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "northscope.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "northscope.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
