{{/*
Resolve the effective push URL. Zot mode: use the in-cluster Service DNS.
External mode: use whatever the operator provided.
*/}}
{{- define "spinup.oci.registryUrl" -}}
{{- if eq .Values.oci.mode "zot" -}}
{{ .Values.oci.zot.service.name }}.{{ .Release.Namespace }}.svc.cluster.local:{{ .Values.oci.zot.service.port }}/spinup
{{- else -}}
{{- required "oci.registryUrl is required when oci.mode != \"zot\"" .Values.oci.registryUrl -}}
{{- end -}}
{{- end -}}

{{/*
Resolve the auth Secret name that the builder Job mounts. Empty string means
"no auth" (anonymous registry).
*/}}
{{- define "spinup.oci.authSecretName" -}}
{{- if .Values.oci.auth.existingSecret -}}
{{ .Values.oci.auth.existingSecret }}
{{- else if .Values.oci.auth.inline.enabled -}}
{{ printf "%s-oci-auth" .Chart.Name }}
{{- end -}}
{{- end -}}
