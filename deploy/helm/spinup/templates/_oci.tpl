{{/*
Resolve the effective push URL. If `oci.registryUrl` is set, it always wins
(needed to route pushes to an externally-exposed Zot so build creds match
the endpoint kubelet later pulls from). Otherwise, in zot mode, fall back
to the in-cluster Service DNS; in external mode it's required.
*/}}
{{- define "spinup.oci.registryUrl" -}}
{{- if .Values.oci.registryUrl -}}
{{ .Values.oci.registryUrl }}
{{- else if eq .Values.oci.mode "zot" -}}
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
