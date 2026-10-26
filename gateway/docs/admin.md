# Running a Fling Gateway: the admin guide

Day-to-day tasks for whoever looks after a gateway. To set one up, start with [Run it on your own server](deploy/docker-compose.md).

## Who is an admin

An account is an admin when either of these is true:

- **The setting:** its email is in `FLING_ADMIN_EMAILS`. That can only be undone by changing the setting.
- **The flag:** it has the admin flag. Set it on **Admin → People** with **Make admin**, or with `fling-gateway user admin <email>` on the server.

Admins see an **Admin** link on the start page. Nobody can turn off or demote their own account, so there's always an admin left.

## People

**Inviting** (local accounts, `FLING_LOCAL_ACCOUNTS=true`):
- Go to **Admin → People**, enter an email address, and click **Invite**. Tick **Admin** to invite another admin.
- **With Resend set up,** the invite is emailed. Otherwise the page shows a link to pass on yourself.
- The link works once, for 7 days. The person opens it and creates a passkey or chooses a password.

**With Microsoft or Google sign-in,** people on the member list (`FLING_MEMBER_EMAILS`, `FLING_MEMBER_DOMAINS`) just sign in. They appear under People after their first sign-in. Invited local accounts can use Microsoft or Google too, if the provider has verified the same email address.

**Each person's row has these buttons:**

| Button | What it does |
|---|---|
| Reset sign-in | Sends, or shows, a 24-hour link to choose a new password or add a passkey. Using it signs the person out everywhere. Local accounts only. |
| Sign out everywhere | Ends all of the person's sessions, in browsers and in Fling on their computers. They can sign in again. |
| Remove codes | Turns off their two-step codes, for example when they've lost their phone and their recovery codes. |
| Make admin / Remove admin | Admin rights. |
| Turn off | Signs them out everywhere and blocks sign-in until you **Turn on** the account again. Their links keep working until they expire. To stop the links too, turn them off under **Links**. |

The same actions are available on the server, for scripts or when nobody can sign in. Run `fling-gateway user help` for the list.

```
docker compose exec gateway fling-gateway user list
docker compose exec gateway fling-gateway user invite ann@example.com
docker compose exec gateway fling-gateway user reset ann@example.com
docker compose exec gateway fling-gateway user disable ann@example.com
docker compose exec gateway fling-gateway user admin ann@example.com
```

## Links

**Admin → Links** lists the organisation's links, newest first. Search by file name or by the email of the person who shared it. Tick **Include expired and turned-off links** to see old ones.

**Turn off** stops a link at once: anyone opening it gets "This link was turned off". The file is deleted from storage a day later (see Clean-up below).

## Rules

**Admin → Rules** sets the rules for the whole organisation. An empty field means "use the server's setting" (the `FLING_*` variables).

| Rule | Notes |
|---|---|
| Default and longest link lifetime | Fling on Windows asks for the lifetime set in its own Settings (1 to 7 days); the gateway shortens anything longer than your maximum. Make sure your storage keeps files at least that long. |
| Largest file | Up to 5 GB for now. |
| Blocked file types | For example `.exe, .msi, .bat`. |
| Only allow these file types | When set, everything else is refused. Add `.zip`: Fling sends folders and several files as one zip. |
| Who can download | **Anyone with the link**, or **only people signed in to this gateway**. Members-only links show a sign-in page first, and Activity records who downloaded. |
| Downloads per link | After that many downloads, the link stops working. |

Upload rules apply to new uploads. Download rules apply to every link at once. When an upload is refused, Fling on Windows shows the reason.

## Activity

**Admin → Activity** is the audit log, newest first:
- sign-ins, including failed ones,
- uploads, links turned off and downloads (with the person's name for members-only links),
- every admin action, including those made with `fling-gateway user` on the server.

It never contains passwords, tokens or link addresses.

## Clean-up

Once an hour, the gateway deletes files from storage:
- files whose link expired or was turned off at least `FLING_DELETE_AFTER` ago (default: a day),
- uploads that were started but never finished.

The storage key needs `s3:DeleteObject` for this. Without it, the gateway logs one warning and leaves the files.

To let a bucket expiry rule do the deleting instead, set `FLING_DELETE_EXPIRED_FILES=false`.

## Backups, updates and monitoring

See the [deployment guide](deploy/docker-compose.md#backups-and-restoring). In short:
- **Backups:** Litestream backs up the database continuously.
- **Updates:** change the image tag and run `docker compose up -d`.
- **Monitoring:** watch `/readyz` with an uptime monitor.

## Troubleshooting

| What you see | What to check |
|---|---|
| `/readyz` says "storage unavailable" | The storage address, bucket name and key. The logs (`docker compose logs gateway`) show the storage's error. |
| Someone gets "You're not a member yet" | Their email isn't on the member list and they have no invited account. Invite them, or add their email or domain. |
| "No verified email address" with Microsoft | The app registration needs the optional claims **email** and **xms_edov** (see the [README](../README.md#microsoft-entra-id)). |
| Invites aren't emailed | `FLING_RESEND_API_KEY` and `FLING_MAIL_FROM` must both be set, on a domain verified in Resend. The People page shows the link to pass on when sending fails. |
| Someone lost their phone and recovery codes | **Remove codes** on their row (or **Reset sign-in**). |
| Fling on Windows says "Your gateway sign-in has ended" | Their session was ended: by Sign out everywhere, by Turn off, or by 90 days without use. They click **Sign in** in Fling's Settings again. |
| Files aren't deleted from storage | Look for the warning about `s3:DeleteObject` in the logs. |
