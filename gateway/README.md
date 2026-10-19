# Fling Gateway

An optional server an organisation hosts between Fling and its S3-compatible storage. It keeps the storage key off people's computers, lets only members sign in, and turns uploads into short links that can be revoked.

- Files go straight from the computer to the storage, through presigned URLs. They never pass through the gateway.
- A share link (`/l/…`) redirects to a five-minute presigned download, so turning a link off takes effect immediately.
- Members sign in with Microsoft, Google, or any other OpenID Connect provider. Fling on Windows signs in through the browser and receives its own token.

Status: sign-in (M2) works. Per-group rules, the admin pages and large-file uploads come later.

## Run it locally

```
docker compose -f compose.dev.yml up --build
```

Open http://localhost:8080 and choose **Continue with Dev sign-in**. On the fake login form, enter any user name and these claims:

```
{"email": "ann@example.com", "email_verified": true}
```

The stack contains the gateway, a local S3 server (Versity Gateway) and a fake sign-in server (mock-oauth2-server). Tests: `go test ./...`. CI also runs `-race`, `go vet` and `gofmt`.

## Configuration

Everything is set through environment variables.

| Variable | Default | Meaning |
|---|---|---|
| `FLING_PUBLIC_URL` | required | The address people use, e.g. `https://fling.example.com`. Must be https, except for localhost. |
| `FLING_LISTEN` | `:8080` | Listen address. |
| `FLING_DATABASE_PATH` | `/data/fling-gateway.db` | SQLite database. Keep `/data` on a volume. |
| `FLING_S3_ENDPOINT`, `FLING_S3_BUCKET`, `FLING_S3_ACCESS_KEY`, `FLING_S3_SECRET_KEY` | required | The storage. The key only needs to read and write objects in that bucket. |
| `FLING_S3_REGION` | `us-east-1` | |
| `FLING_S3_PATH_STYLE` | `true` | Path-style addressing (MinIO, B2, R2). |
| `FLING_DEFAULT_LINK_TTL`, `FLING_MAX_LINK_TTL` | `168h`, `720h` | How long links last, by default and at most. |
| `FLING_MAX_UPLOAD_BYTES` | `2147483648` | Largest upload (2 GiB). |
| `FLING_MEMBER_EMAILS` | | Comma-separated addresses that may sign in. |
| `FLING_MEMBER_DOMAINS` | | Comma-separated domains whose addresses may sign in, e.g. `example.com`. |
| `FLING_MICROSOFT_CLIENT_ID`, `FLING_MICROSOFT_CLIENT_SECRET` | | Sign in with Microsoft. |
| `FLING_MICROSOFT_TENANT` | `common` | `common` (work, school and personal accounts), `organizations`, `consumers`, or one tenant ID. |
| `FLING_GOOGLE_CLIENT_ID`, `FLING_GOOGLE_CLIENT_SECRET` | | Sign in with Google. |
| `FLING_OIDC_ISSUER`, `FLING_OIDC_CLIENT_ID`, `FLING_OIDC_CLIENT_SECRET` | | Any other OpenID Connect provider (Okta, Keycloak, Authentik, …). |
| `FLING_OIDC_NAME` | `single sign-on` | Button label for that provider. |
| `FLING_DEV_TOKEN` | | Development only: a bearer token (32+ characters) that acts as `FLING_DEV_USER_EMAIL` without signing in. Leave it unset in production. |

At least one sign-in provider (or the development token) is required. With a provider configured, at least one member email or domain is required. Removing someone from the member list and restarting the gateway ends their access, including tokens already issued.

## Sign-in providers

Each provider redirects back to `<FLING_PUBLIC_URL>/auth/callback/<provider>`, where `<provider>` is `microsoft`, `google` or `oidc`.

### Microsoft (Entra ID)

1. In the [Entra admin center](https://entra.microsoft.com), go to **App registrations → New registration**.
   - **Supported account types:** "Accounts in any organizational directory and personal Microsoft accounts", or only your organisation.
   - **Redirect URI:** platform **Web**, `https://<your gateway>/auth/callback/microsoft`.
2. **Token configuration → Add optional claim → ID:** tick **email** and **xms_edov**.
   - Without `xms_edov`, only personal Microsoft accounts can sign in. The gateway trusts a work or school account's email only when Microsoft confirms that the account's organisation owns the email domain. Otherwise any organisation's admin could give a user someone else's address.
3. **Certificates & secrets → New client secret.** Put the Application (client) ID and the secret value in `FLING_MICROSOFT_CLIENT_ID` and `FLING_MICROSOFT_CLIENT_SECRET`.

### Google

1. In the [Google Cloud console](https://console.cloud.google.com), create a project.
2. Under **Google Auth Platform**, set up the consent screen:
   - User type **External**, or **Internal** for a Google Workspace organisation.
   - While the app is in **Testing**, only the test users you add can sign in.
3. Go to **Clients → Create client → Web application** and add the authorized redirect URI `https://<your gateway>/auth/callback/google`.
4. Put the client ID and secret in `FLING_GOOGLE_CLIENT_ID` and `FLING_GOOGLE_CLIENT_SECRET`.

Google accounts sign in only when Google reports the email as verified.

## How Fling on Windows signs in

This follows RFC 8252 (a loopback redirect plus PKCE):

1. Fling starts a one-time listener on `127.0.0.1:<random port>`.
2. It opens the browser at `/auth/desktop?redirect_uri=http://127.0.0.1:<port>/…&state=…&code_challenge=…&code_challenge_method=S256`.
3. The user signs in if needed, then confirms with **Continue**. The gateway redirects to the listener with a one-time code, valid for two minutes.
4. Fling posts `grant_type=authorization_code`, `code`, `code_verifier` and `redirect_uri` to `/api/v1/token`. It gets a bearer token that lasts 90 days and is extended while in use.
5. `DELETE /api/v1/session` signs that computer out.

Tokens, cookies and codes are stored only as SHA-256 hashes. Request logs contain route patterns, never link slugs, tokens or email addresses.
