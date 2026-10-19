# Fling privacy policy

*Last updated: 19 October 2026*

Fling is a Windows app that uploads the files you choose to storage that **you** configure, and copies a download link for you to share. This policy explains what happens to your data.

## What Fling collects

Nothing. Fling has no account, no telemetry, no analytics and no advertising. The people who make Fling don't run a server that your files or details pass through.

## Your files

When you choose "Fling it" on a file or folder, Fling uploads it straight from your computer to the S3-compatible storage you entered in Settings, for example Amazon S3, Backblaze B2, Cloudflare R2 or your own MinIO server. The provider you choose stores the file, under that provider's own terms and privacy policy. Anyone who has the download link can download the file until the link expires.

Several files or a folder are first zipped into a temporary file on your computer. Fling deletes that temporary file after the upload.

## Data stored on your computer

Fling keeps these files in its own app data folder on your PC:

- **Settings**, including your storage address, bucket name, access key and secret key.
- **Upload history**: file names and where they were on your PC, sizes, dates and the links Fling created, so you can copy them again. The history keeps the 500 most recent uploads.
- **A troubleshooting log**. It records what Fling did and any errors, but not your keys, links or file names.

None of this leaves your computer unless you choose to share it. "Copy diagnostic info" in Settings puts a summary of the log on your clipboard, so you can paste it into a bug report yourself. Uninstalling Fling removes all of these files.

## Microsoft

If you install Fling from the Microsoft Store, Microsoft may collect crash reports and install statistics under the [Microsoft Privacy Statement](https://privacy.microsoft.com/privacystatement). The Fling developers can only see these reports in aggregate in Microsoft Partner Center.

## Changes

If this policy changes, the new version will be published at this address with a new date.

## Contact

Questions or concerns: open an issue at <https://github.com/ulvs/fling/issues>.
