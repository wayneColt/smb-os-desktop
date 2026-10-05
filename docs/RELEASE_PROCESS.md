# Release process

From a commit on `main` to a public download someone else has checked, and then the post. Every step
ends in a **receipt**: something a second person can re-read later (a commit, a URL that answers, a file,
a command's saved output). An exit code on its own is not a receipt.

Receipts for a release are collected in `marketing/receipts/vX.Y.Z.md`, one section per step, in the
order below. A step whose receipt is missing has not happened.

Throughout, `X.Y.Z` is the version (`0.1.0` for the first release) and the repository is
`github.com/wayneColt/smb-os-desktop`.

## 0. Before tagging

- `main` is green: `go test ./...` passes on the commit you will tag.
- The version in the code matches: `go run ./cmd/aios --version` prints `X.Y.Z`, and
  `GET /api/health` reports `"version":"X.Y.Z"`.
- `marketing/RELEASE_NOTES_vX.Y.Z.md` is written and every claim in it is in `marketing/CLAIMS.md`.
- `scripts/firewall_scan.sh` is GREEN on the commit, as a full pass (the private term list present, not
  `FIREWALL_GENERIC_ONLY`), with screenshots either read by OCR or checked by eye.

**Receipt:** the commit SHA, the `go test` summary line, the `--version` output, and the scanner's verdict
line (it prints file, line and category only, never the matched text, so it is safe to keep).

## 1. Tag

```sh
git tag -a vX.Y.Z -m "SMB OS Desktop X.Y.Z"
git push origin vX.Y.Z
```

A published tag never moves. If something is wrong after the tag is pushed, fix it and release
`X.Y.(Z+1)`.

**Receipt:** `git rev-parse vX.Y.Z^{}` (the tagged commit) and the tag's URL on GitHub.

## 2. CI builds

The tag push starts the release workflow. It runs the tests, then builds `aios` for windows/amd64,
darwin/arm64, darwin/amd64, linux/amd64 and linux/arm64, and packages each as
`smb-os-desktop_X.Y.Z_<os>_<arch>.zip` (the binary plus `README.txt`), plus a version-less copy
`smb-os-desktop_<os>_<arch>.zip` with the same bytes.

**Receipt:** the workflow run URL and its conclusion:
`gh run list --workflow <release workflow> --limit 1 --json url,conclusion,headSha`. The `headSha` must
equal the tagged commit.

## 3. GitHub release

The workflow publishes the release `vX.Y.Z` with the five versioned zips, their five version-less
copies, `SHA256SUMS.txt`, `install.sh` and `install.ps1`, and uses `marketing/RELEASE_NOTES_vX.Y.Z.md`
as the release text. The version-less names are what `releases/latest/download/<name>` resolves, so the
README, the solutions page and the installers always point at the newest release without edits.

**Receipt:** `gh release view vX.Y.Z --json url,assets --jq '.url, (.assets[] | .name + " " + (.size|tostring))'`.
The asset list must be exactly those thirteen names, none of them empty, and each version-less zip
must have the same SHA-256 as its versioned twin.

## 4. Checksums

Download every asset from its **public** URL (not from the CI run's artifacts) and check it:

```sh
base=https://github.com/wayneColt/smb-os-desktop/releases/download/vX.Y.Z
curl -fsSLO "$base/SHA256SUMS.txt"
for f in $(awk '{print $2}' SHA256SUMS.txt); do curl -fsSLO "$base/$f"; done
sha256sum -c SHA256SUMS.txt
```

Then check that the `latest` links resolve to this release. The two hashes printed must be equal:

```sh
curl -fsSL https://github.com/wayneColt/smb-os-desktop/releases/latest/download/smb-os-desktop_linux_amd64.zip | sha256sum
grep ' smb-os-desktop_linux_amd64.zip$' SHA256SUMS.txt
```

**Receipt:** the `sha256sum -c` output, one `OK` line per zip (all ten), and the `latest` check.

## 5. Installers

Run each one-liner against the published release on a machine of that family:

```sh
curl -fsSL https://github.com/wayneColt/smb-os-desktop/releases/latest/download/install.sh | sh
```

```powershell
irm https://github.com/wayneColt/smb-os-desktop/releases/latest/download/install.ps1 | iex
```

Check that the installer picked the right file, verified it against `SHA256SUMS.txt`, started it, and
that the program printed its one start-up line.

**Receipt:** per OS: the installer's transcript (file chosen, checksum verified), the start-up line, and
`curl -s http://127.0.0.1:7701/api/health` showing `"version":"X.Y.Z"` and `"chain_ok":true`.

## 6. Solutions page update

- The download links need no edit: they use `releases/latest/download/` and the version-less names.
  Edit the page only if what it says about the product changed (the byline's version, a new limit, a
  new claim), and then add those claims to `marketing/CLAIMS.md` first.
- Update `site/solutions/manifest.entry.json` if the title or description changed.
- Regenerate the screenshots in the page and the link card: `python3 marketing/tools/site_images.py`
  (writes `site/solutions/smb-os-desktop/images/*.png` and the 1200×630 `og.png`).
- Copy `site/` into the site repository, add or replace the manifest entry, let the site's publisher
  regenerate the directory index and sitemap (it runs its own term check before it writes), run
  `scripts/firewall_scan.sh` over the copied pages, and publish.

**Receipt:** the site commit SHA, and the live page answering 200 with the new version string in it:
`curl -fsS https://waynecolt.com/solutions/smb-os-desktop/ | grep -c 'X.Y.Z'`.

## 7. Check the public download from a clean machine

On a computer that has never run SMB OS Desktop and is not on the network the release was built on
(a fresh virtual machine, a hosted CI runner, or someone else's laptop):

1. Open the solutions page in a browser. The main button must name this computer's system.
2. Download, then compare the file's SHA-256 with its line in `SHA256SUMS.txt`.
3. Unzip and double-click. Expect the first-run prompt on macOS and Windows (unsigned), as the README
   describes, and expect it to be enough to follow the README to get past it.
4. The desktop opens with the "Fictional demo data" mark. Run the morning brief. Run Submit payroll and
   check that it waits for approval; approve it once and check the second approval is refused.
5. Open the Receipts window: the chain checks out.
6. Open the solutions page on a phone: it says to download on a computer and nothing on it is broken.

**Receipt:** date, system and processor, the file's SHA-256, the `/api/health` output, one screenshot of the
desktop and one of the phone view.

## 8. Post

Follow [MARKETING_PACKAGE.md](MARKETING_PACKAGE.md): the post's receipt page is live, the exact post
text has a person's approval, and it is posted once.

**Receipt:** the post's URL, added to the post receipt page and to `marketing/receipts/vX.Y.Z.md`.

## If a step fails

- Before step 8: fix it, and if the tag is already public, release a new patch version. Delete a broken
  GitHub release only if nobody could have downloaded it yet, and say so in the receipts.
- After step 8: publish a correction. Do not delete the post or the release; edit the release notes to
  say what was wrong and which version fixes it.
