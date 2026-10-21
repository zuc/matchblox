# Releasing

A merge to `main` that CI passed makes a release. Nobody tags by hand.

1. **Release** (`release.yml`) picks the next version from the conventional
   commits (`feat` → minor, `fix` → patch; before 1.0 a breaking change is a
   minor), makes the draft release `vX.Y.Z` and pushes its tag on the
   commit CI verified. Its notes are the `## [Unreleased]` section of
   `CHANGELOG.md`, so write that section in the pull request.
2. **Publish** (`publish.yml`) builds `matchblox-{linux,darwin}-{amd64,arm64}`
   with a `.sha256`, a CycloneDX SBOM and provenance and SBOM attestations,
   attaches all twelve files to the draft and publishes it as a pre-release.
   The repository's releases are immutable: once published, a release takes
   no more assets, so the draft is what lets them in. Then it installs the release with `curl -fsSL https://matchblox.sh | sh` on
   Linux and macOS, amd64 and arm64, and checks `matchblox version`. Then it
   makes it the latest release, checks the install with no version named,
   and publishes its docs to docs.matchblox.com. Until then the install
   script keeps installing the release before it.
3. A pull request `docs: vX.Y.Z is released` moves `[Unreleased]` under the
   version and turns the README, the docs and the site to the released
   install. It merges itself once its checks pass, and the release job keeps
   it up to date with `main` for 40 minutes. Until it merges, Lint fails on
   other pull requests.

## Turn releases on or off

Releases run while the repository variable `RELEASE_ENABLED` is `true`:

```sh
gh variable set RELEASE_ENABLED --body true -R blox-eng/matchblox
```

Any other value, or no variable, holds them.

## The release token

`RELEASE_TOKEN` is a fine-grained token for this repository only, with
Contents and Pull requests set to read and write. When it is missing or
expired, the Create Release job fails and says so. To replace it:

1. GitHub → Settings → Developer settings → Fine-grained tokens → Generate
   new token. Resource owner `blox-eng`, only `blox-eng/matchblox`.
2. `gh secret set RELEASE_TOKEN -R blox-eng/matchblox`, and paste it.
3. Re-run the failed Release run.

## The push relay's secrets

The relay on `matchblox.sh/push/*` (`worker/push.js`) signs pushes with the
matchblox APNs key and makes grants with an HMAC secret. They live in the
Worker's secrets, never in the repository:

| Secret | What |
|---|---|
| `APNS_KEY` | The APNs auth key: the whole `.p8` file, PEM lines and all |
| `APNS_KEY_ID` | The key's 10-character Key ID |
| `APNS_TEAM_ID` | The 10-character Team ID |
| `APNS_BUNDLE_ID` | The app's bundle id |
| `GRANT_SECRET` | A random secret for the grants, kept in a file only you can read |

```sh
npx wrangler secret put APNS_KEY < AuthKey_XXXXXXXXXX.p8
npx wrangler secret put APNS_KEY_ID
npx wrangler secret put APNS_TEAM_ID
npx wrangler secret put APNS_BUNDLE_ID
# Wrangler never shows a secret again, so keep GRANT_SECRET: the live check
# below needs it. Keep a copy in the password manager too.
(umask 077; mkdir -p ~/.config/matchblox && openssl rand -hex 32 | tr -d '\n' > ~/.config/matchblox/grant-secret)
npx wrangler secret put GRANT_SECRET < ~/.config/matchblox/grant-secret
```

If the value is lost, make a new file the same way and put it again before
the live check (this voids every grant, as below).

A new `GRANT_SECRET` voids every grant: each phone gets new ones the next
time the app opens. The optional variable `APNS_ENV = "development"` sends to
Apple's sandbox and accepts development App Attest keys (a build from Xcode);
without it the relay is production.

### Rate rules

`wrangler.jsonc` binds three Workers rate limiters, which the relay checks
in code and answers `429` over the limit. Cloudflare counts each per
location, over 60 seconds:

| Binding | Route | Key | Limit |
|---|---|---|---|
| `RL_SEND` | `/push/send` | the target (`<kind>:<token or channel>`), after the grant check | 30 a minute |
| `RL_GRANT` | `/push/grant` | the caller's IP | 10 a minute |
| `RL_CHANNEL` | `/push/channel` | the caller's IP, and the attested key after its check | 3 a minute each |

They deploy with the Worker; there is nothing to set in the dashboard. A
binding that is missing or fails limits nothing, so a mistake here never
stops pushes. Apple allows 10,000 channels for each app and environment:
`all-channels` (Apple's channel management API) shows how many there are.

### Live check (gates a release that ships the island)

The relay reaches Apple over HTTP/2, and channel management on port 2196
(sandbox 2195); the tests cannot reach either. Before the first release that
ships the island, and after any change to `worker/push.js`, both checks
below pass against Apple's sandbox, with a development build of the app on
the iPad.

1. Deploy to the sandbox (the secrets above are set):

   ```sh
   npx wrangler@4.148.0 deploy --var APNS_ENV:development
   npx wrangler@4.148.0 tail --format json \
     | jq -c 'select(.event.request.url|test("/push/")) | {url: .event.request.url, status: .event.response.status}'
   ```

   `tail` shows each call's path and status, never its body.

2. **Channel create.** Open the development build and follow a host
   (Settings → `Follow`). `tail` shows `/push/grant` and `/push/channel`
   with status `200`: the relay made a channel at Apple. Then the island
   appears for that host and moves with it (a start, then updates through
   the channel).

3. **Device send.** With the device token of that build (the host keeps it
   in the phone's push registration, `device_token`) and the
   `GRANT_SECRET` kept in `~/.config/matchblox/grant-secret` (Node reads it
   from the file, so the secret is never in a command line or `ps`):

   ```sh
   TOKEN=<device token, hex>
   GRANT=$(TOKEN="$TOKEN" node -e 'const c = require("crypto"), fs = require("fs");
     const k = fs.readFileSync(process.env.HOME + "/.config/matchblox/grant-secret", "utf8").trim();
     process.stdout.write(c.createHmac("sha256", k).update("device:" + process.env.TOKEN).digest("hex"))')
   curl -sS https://matchblox.sh/push/send -H 'Content-Type: application/json' \
     -d '{"target":"'"$TOKEN"'","kind":"device","grant":"'"$GRANT"'","push_type":"alert","priority":10,"payload":{"aps":{"alert":{"title":"matchblox","body":"relay live check"}}}}'
   ```

   It answers `{"status":200}` and the alert shows on the iPad. Any other
   status is Apple's, with its `reason`.

4. Back to production: `npx wrangler@4.148.0 deploy` (no `--var`).

If either check fails, the island does not ship: `push: relay <status>`
on the host's Machine tab names the same status.

## When something fails

| What failed | What to do |
|---|---|
| Publish, from a runner or network hiccup | Publish the tag again: `gh workflow run publish.yml --ref vX.Y.Z` |
| Publish says the release is published without a file | It went out before its binaries and cannot take them now. Merge a fix: the next release carries them |
| Publish, from a bug in the code or the workflow | Merge the fix: the next release is published and becomes the latest. The broken one stays a pre-release |
| The changelog pull request did not merge itself | Update it with `main` (`gh pr update-branch <branch>`), then merge it once its checks pass |
| No changelog pull request after a release | Run `.github/scripts/promote-changelog.sh X.Y.Z <date>`, `VERSION=vX.Y.Z .github/scripts/flip-release.sh` and `.github/scripts/announce-release.sh X.Y.Z`, and open the pull request yourself |
| The changelog pull request says the news line is stale, and Lint fails | The release's changelog section has no `- **headline**`: add one, run `.github/scripts/announce-release.sh X.Y.Z` on the branch, push, and merge |
| Lint says `news-guard: the news line … does not name vX.Y.Z` | The news line in `www/index.html` was edited by hand, or the headline changed after the release. Run `.github/scripts/announce-release.sh X.Y.Z` and commit `www/index.html` |
| The docs of a tag | Publish the tag again: the docs deploy after its verify job |
| The docs site shows an older build of the release's commit | Merge the next fix: its tag deploys the docs. GitHub Pages keeps the first deployment of a commit, so publishing the same tag again does not replace it |

Tags are immutable: a bad release is followed by a fixed one, never moved.
