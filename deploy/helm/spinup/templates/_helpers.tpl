{{- define "spinup.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "spinup.controlPlane.name" -}}
{{- printf "%s-control-plane" .Chart.Name -}}
{{- end -}}

{{- define "spinup.labels" -}}
app.kubernetes.io/name: {{ include "spinup.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
