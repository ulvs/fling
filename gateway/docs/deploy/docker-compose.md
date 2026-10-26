# Run Fling Gateway on your own server (Docker Compose)

This sets up the gateway on any Linux server with Docker: a small VPS, a homelab machine, or a VM at your company. You get:

- **the gateway** itself,
- **Caddy** in front, which gets and renews the HTTPS certificate on its own,
- **Litestream**, which backs up the gateway's database to a bucket every second.

The files are in [`compose/`](compose/). This exact setup was tested from start to finish: start, sign-in page through Caddy, invite, upload and download, backup, and restoring the backup.

## What you need

- A Linux server with Docker and the Compose plugin, with ports 80 and 443 open to the internet.
- A domain name, such as `fling.example.com`, pointing to the server.
- S3-compatible storage: Amazon S3, Backblaze B2, Cloudflare R2, Wasabi, MinIO or Garage. You need two buckets, one for the shared files and one for the backup.
- Optional: a [Resend](https://resend.com) account, to email invites and password resets.

## 1. Storage

Create two **private** buckets, for example `fling-gateway` and `fling-gateway-backup`.

**A key for the gateway**, limited to the files bucket. In AWS policy language (MinIO and most others accept the same):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": ["s3:ListBucket", "s3:GetBucketLocation"], "Resource": ["arn:aws:s3:::fling-gateway"] },
    { "Effect": "Allow", "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject", "s3:AbortMultipartUpload"], "Resource": ["arn:aws:s3:::fling-gateway/*"] }
  ]
}
```

The gateway deletes files a day after their link ends. If you'd rather leave that to an expiry rule on the bucket, leave out `s3:DeleteObject` and set `FLING_DELETE_EXPIRED_FILES=false`. The rule must keep files at least as long as your longest link lifetime.

**A second key for Litestream**, limited to the backup bucket, with list, read, write and delete. Don't put an expiry rule on the backup bucket.

## 2. The files

On the server:

```
mkdir fling-gateway && cd fling-gateway
curl -fsSLO https://raw.githubusercontent.com/ulvs/fling/main/gateway/docs/deploy/compose/compose.yml
curl -fsSLO https://raw.githubusercontent.com/ulvs/fling/main/gateway/docs/deploy/compose/Caddyfile
curl -fsSLO https://raw.githubusercontent.com/ulvs/fling/main/gateway/docs/deploy/compose/litestream.yml
curl -fsSL -o fling-gateway.env https://raw.githubusercontent.com/ulvs/fling/main/gateway/docs/deploy/compose/fling-gateway.env.example
curl -fsSL -o litestream.env https://raw.githubusercontent.com/ulvs/fling/main/gateway/docs/deploy/compose/litestream.env.example
chmod 600 fling-gateway.env litestream.env
```

Then edit:

- **`Caddyfile`:** replace `fling.example.com` with your domain.
- **`fling-gateway.env`:**
  - your address, the files bucket and its key,
  - `FLING_ADMIN_EMAILS` (you),
  - the Resend key and sender, if you have them.

  Every setting is described in the [gateway README](../../README.md#configuration).
- **`litestream.env`:** the backup bucket and its key.
- **`compose.yml`:** pin the gateway image for production. Replace `latest` with `sha-<commit>`, or with the image digest from the [package page](https://github.com/ulvs/fling/pkgs/container/fling-gateway). Then updates only happen when you change it.

## 3. Start it

```
docker compose up -d
docker compose ps
curl https://fling.example.com/readyz      # "ready" once the database and storage are reachable
```

The first start takes a minute while Caddy gets the certificate.

## 4. Invite yourself

```
docker compose exec gateway fling-gateway user invite you@example.com
```

- **With Resend set up:** the invite is emailed.
- **Either way:** the link is printed.

Open it, then choose **Create a passkey** (or a password). Because your address is in `FLING_ADMIN_EMAILS`, you're an admin. Invite everyone else from **Admin → People**.

For sign-in with Microsoft or Google, see [Sign-in providers](../../README.md#sign-in-providers).

## 5. Connect Fling on Windows

In Fling, go to **Settings → Where files go** and choose **My organisation's Fling Gateway**. Enter your address and click **Sign in**.

## Backups and restoring

Litestream copies every change to the backup bucket within a second.

**Check a backup** at any time without touching the running gateway. This restores a copy, then lists its accounts:

```
docker compose run --rm --no-deps litestream restore -o /data/restored.db /data/fling-gateway.db
docker compose run --rm --no-deps -e FLING_DATABASE_PATH=/data/restored.db gateway user list
```

**Restore for real**, after a broken disk or on a new server with the same files:

1. Stop the gateway and Litestream:

   ```
   docker compose stop gateway litestream
   ```

2. Replace the database with the backup. This **deletes the current database** and its `-wal` and `-shm` files, then restores the backup in its place:

   ```
   docker compose run --rm --no-deps --entrypoint sh litestream -c \
     'rm -f /data/fling-gateway.db /data/fling-gateway.db-wal /data/fling-gateway.db-shm && litestream restore /data/fling-gateway.db'
   ```

3. Start everything again:

   ```
   docker compose up -d
   ```

## Updating

1. Read the release notes for the new version.
2. Change the image tag or digest in `compose.yml`.
3. Run:

```
docker compose pull gateway
docker compose up -d gateway
docker compose logs -f gateway     # database migrations run on start; then "listening"
```

The database is backed up continuously, so there's no extra step before an update. To go back, put the old tag back. Database migrations only add things, so an older version still starts, but it ignores what it doesn't know.

## Monitoring

`GET /readyz` answers `ready` (200) when the database and storage can be reached, and 503 otherwise. Point an uptime monitor at it. The logs (`docker compose logs gateway`) are JSON. They show routes and outcomes, never tokens, link addresses or email addresses.
