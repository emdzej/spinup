# Toolbox — a SpinUP example app

Five small stateless HTTP functions modeled after CyberChef's most-used
recipes:

| Function | POST body | Response |
|---|---|---|
| `hex-to-text` | hex string (with or without whitespace) | decoded UTF-8 |
| `text-to-hex` | any UTF-8 text | lowercase hex |
| `base64-encode` | any UTF-8 text | standard base64 |
| `base64-decode` | standard or url-safe base64 | decoded UTF-8 |
| `jwt-decode` | a JWT string | pretty JSON `{ header, payload, signature }` |

Every function is a TypeScript component targeting `@fermyon/spin-sdk`
(runtime language `TS` in SpinUP).

## Import

1. In the SpinUP UI, create a new **Application** named `toolbox`
   (language: `TypeScript`, runtime: `spinapp`).
2. For each folder under `examples/toolbox/`, create a **Function** with
   the folder name (e.g. `hex-to-text`) and route `/`.
3. Paste `src/index.ts` into the Monaco editor for that function. If
   you'd rather import the whole folder as a tarball, `tar czf src.tgz -C
   <folder> .` and POST it to
   `/api/v1/applications/{appId}/functions/{fnId}/source.tar.gz`.
4. Trigger a build. Once the build succeeds and the SpinApp is Ready,
   the function is publicly reachable at
   `https://<function-name>.spinup.solvely.pl/`.

## Try it

```bash
# encode
curl -sX POST https://base64-encode.spinup.solvely.pl/ -d 'hello world'
# → aGVsbG8gd29ybGQ=

# decode a JWT
curl -sX POST https://jwt-decode.spinup.solvely.pl/ \
  -d 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhbGljZSJ9.'
# → { "header": {...}, "payload": {"sub":"alice"}, "signature": "" }
```

## Extending

Adding a new tool is: `mkdir <name>/src`, drop an `index.ts` that
implements `handleRequest`, add it as a Function in the same Application.
