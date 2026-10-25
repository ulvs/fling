# Fling

**Right-click any file, choose "Fling it", and get a download link.**

Fling is a small Windows app that puts "Fling it" in the File Explorer menu. It uploads the file straight to storage you control, and copies a download link to your clipboard, ready to paste into an email, chat or ticket.

- **Your storage:** Amazon S3, Backblaze B2, Cloudflare R2, Wasabi, MinIO, or any other S3-compatible service. There's no Fling account, no middleman server, and no telemetry.
- **Folders and several files at once:** zipped into one link, or a separate link for each file if you prefer.
- **Expiring links:** they last 1 to 7 days, your choice.
- **Large files:** uploaded in parallel parts, with progress, speed and time left.
- **History:** every link you've made, ready to copy again.
- **Works everywhere you right-click:** the Windows 11 menu, the classic menu, and file managers such as Total Commander.
- **Fling Gateway (optional):** for organisations. A server you host keeps the storage keys, lets only members sign in, and gives out short links that can be turned off.

Fling is free and open source under the [MIT License](LICENSE).

## Install

Fling needs Windows 10 (1809) or Windows 11, on x64. ARM PCs run it through emulation.

**Microsoft Store.** The listing is "Fling - File Sender" (product ID `9PK2KJ77PLF6`), currently in certification. Once it's published, it's at <https://apps.microsoft.com/detail/9PK2KJ77PLF6>. The Store version installs the Windows App SDK runtime for you and updates itself.

**GitHub Releases.** Releases with a sideloadable `.msix` will appear under [Releases](../../releases). Until then, use the Store or build it yourself (see below).

After installing, open **Fling** from the Start menu. It starts on **Settings**, where you choose where your files go.

## Set up your storage

Under **Settings → Where files go**, choose **My own storage (S3-compatible)**, then fill in the endpoint, region, bucket, access key and secret key. **Test connection** checks them.

1. **Create a private bucket.** Don't make it public: Fling's links are presigned, so they work without public access, and they stop working when they expire.
2. **Create an access key that can only use that bucket.** Fling needs:
   - `s3:PutObject` and `s3:GetObject` on the bucket's objects,
   - `s3:ListBucket` on the bucket, for **Test connection**,
   - `s3:AbortMultipartUpload` on the objects, so cancelled large uploads can be cleaned up.
3. **Optional:** add a lifecycle rule that deletes files after 7 days. Links never last longer than that, so nothing useful is lost.

| Service | Endpoint | Region | Notes |
|---|---|---|---|
| Backblaze B2 | `https://s3.<region>.backblazeb2.com` | e.g. `eu-central-003` | Free tier: 10 GB of storage. Use an *application key* limited to the bucket. |
| Cloudflare R2 | `https://<account-id>.r2.cloudflarestorage.com` | `auto` | Free tier: 10 GB of storage, no download fees. Use an *R2 API token* with object read and write on the bucket. |
| Amazon S3 | `https://s3.<region>.amazonaws.com` | e.g. `eu-north-1` | An IAM user with a policy limited to the bucket. |
| MinIO, Garage, Versity, … | your server, e.g. `https://s3.example.com` | usually `us-east-1` | Self-hosted. |

Fling uses path-style addressing (`https://host/bucket/file`) for every service except Amazon S3, so self-hosted servers work without per-bucket DNS names.

The settings, including the secret key, are stored in Fling's own app data folder on your PC. See the [privacy policy](PRIVACY.md).

## Use a Fling Gateway

If your organisation runs a [Fling Gateway](gateway/README.md):

1. In **Settings → Where files go**, choose **My organisation's Fling Gateway**.
2. Enter its address, for example `fling.example.com`.
3. Click **Sign in**. Your browser opens. Sign in the way your organisation set up (passkey, password, Microsoft or Google), then click **Continue**.

From then on, "Fling it" uploads through the gateway. You don't need a storage key; the sign-in is stored in Windows Credential Manager. Your links appear on the gateway's start page, where they can be turned off.

## Fling Gateway packages (for administrators)

The gateway is a single Go service, published as a Docker image for amd64 and arm64:

```
docker pull ghcr.io/ulvs/fling-gateway:latest
```

| Tag | Contents |
|---|---|
| `latest` | The newest build from `main`. |
| `sha-<commit>` | A specific commit. Pin this, or the image digest, in production, so updates only happen when you choose. |

It needs S3-compatible storage, a volume for its SQLite database, and HTTPS in front of it (Caddy, Traefik, nginx, or a cloud load balancer). Configuration, sign-in providers, local accounts, the admin commands and a Docker Compose example are in [gateway/README.md](gateway/README.md).

To try it locally, with a test S3 server and a fake sign-in provider:

```
cd gateway
docker compose -f compose.dev.yml up --build
```

Then open <http://localhost:8080>.

## Build from source

Fling is C++20 with C++/WinRT and WinUI 3 (Windows App SDK 2.5), packaged as MSIX.

**You need:**
- Visual Studio 2026 (version 18) with the *Desktop development with C++* and *WinUI application development* workloads (v145 toolset).
- [vcpkg](https://github.com/microsoft/vcpkg), integrated with `vcpkg integrate install`. Dependencies (aws-sdk-cpp, nlohmann-json, minizip-ng, gtest) come from `vcpkg.json` and build on the first run.
- **Developer Mode** in Windows settings, to install your own builds.

**Build and install for yourself:**

```
msbuild Fling.sln -restore -p:Configuration=Release -p:Platform=x64
```
```
Add-AppxPackage -Register .\x64\Release\Fling\AppxManifest.xml
```

Fling then appears in the Start menu and in the File Explorer menu. To remove it:

```
Get-AppxPackage MocapMech.Fling-FileSender | Remove-AppxPackage
```

`Fling.exe` can't be started on its own; it needs the package identity that installing gives it.

**Tests:**

```
msbuild Fling.Tests/Fling.Tests.vcxproj -p:Configuration=Debug -p:Platform=x64
```
```
Fling.Tests\x64\Debug\Fling.Tests.exe
```

Two groups of integration tests run only when their local test servers are up; otherwise they're skipped:
- **S3 tests:** need `docker compose up -d` in the repo root.
- **Gateway tests:** need `gateway/compose.dev.yml` running.

The two can't run at the same time, because both use port 9000.

**The gateway:** `cd gateway` then `go test ./...` (Go 1.26). CI also runs `go vet`, `gofmt` and the race detector.

## Repository layout

| Path | What it is |
|---|---|
| `Fling/` | The Windows app: windows, pages and controls (`Pages/`, `Controls/`), uploads, settings and history (`Core/`), and the design system (`Design/`). |
| `FlingShellExt/` | The File Explorer menu handler (a small COM DLL, packaged with the app). |
| `Fling.Tests/` | Unit and integration tests (GoogleTest). |
| `gateway/` | Fling Gateway: the optional server for organisations (Go). |
| `PRIVACY.md` | Privacy policy. |

## Contributing and support

Bug reports and ideas: [GitHub issues](../../issues). For a problem with the app, click **Copy diagnostic info** under **Settings → Troubleshooting** and paste the result into the issue. It contains no keys, links or file names.

## License

[MIT](LICENSE) © 2026 MocapMech
