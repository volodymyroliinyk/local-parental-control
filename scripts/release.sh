#!/usr/bin/env bash
set -Eeuo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
remote=origin
source_branch=develop
release_branch=main
assume_yes=false
draft=false
dry_run=false

usage() {
  cat <<'EOF'
Create and publish a Local Parental Control release.

Usage:
  ./scripts/release.sh VERSION [--draft] [--yes]
  ./scripts/release.sh VERSION --dry-run

Arguments:
  VERSION    Semantic version without a v prefix, for example 0.2.0

Options:
  --draft    Create the GitHub release as a draft
  --dry-run  Run preflight checks, tests, and builds without changing Git
  --yes      Skip the final interactive confirmation
  -h, --help Show this help

The release command:
  1. verifies the repository, branches, changelog, tools, and GitHub login;
  2. runs gofmt verification, go vet, race tests, and shell syntax checks;
  3. moves Unreleased changelog entries under the release version;
  4. creates a release commit and annotated vVERSION tag;
  5. builds static Linux binaries, a tar archive, and a Debian package;
  6. pushes develop, main, and the tag atomically;
  7. creates a GitHub Release with artifacts and SHA-256 checksums.
EOF
}

current_step="initialization"
step() { current_step=$*; printf '\n==> %s\n' "$*"; }
fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
on_error() {
  status=$?
  line=$1
  trap - ERR
  printf '\nERROR: Release stopped during "%s" (line %s, exit code %s).\n' "${current_step}" "${line}" "${status}" >&2
  printf 'No automatic rollback was performed. Inspect git status, local tags, and the GitHub release before retrying.\n' >&2
  exit "${status}"
}
trap 'on_error ${LINENO}' ERR

[[ $# -ge 1 ]] || { usage >&2; exit 2; }
if [[ $1 = "-h" || $1 = "--help" ]]; then usage; exit 0; fi
version=$1
shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --draft) draft=true ;;
    --dry-run) dry_run=true ;;
    --yes) assume_yes=true ;;
    -h|--help) usage; exit 0 ;;
    *) fail "Unknown option: $1" ;;
  esac
  shift
done

if [[ ! ${version} =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  fail "VERSION must follow Semantic Versioning and must not include a v prefix."
fi

tag="v${version}"
architecture=$(dpkg --print-architecture)
deb_name="local-parental-control_${version}_${architecture}.deb"
archive_name="local-parental-control_${version}_linux_${architecture}.tar.gz"
checksums_name="local-parental-control_${version}_SHA256SUMS"
notes_file="${project_dir}/dist/release-notes-${version}.md"

cd "${project_dir}"

step "Checking required tools"
for tool in git go gh dpkg dpkg-deb tar sha256sum awk; do
  command -v "${tool}" >/dev/null 2>&1 || fail "Required command not found: ${tool}"
done
printf 'Go: %s\n' "$(go version)"
printf 'GitHub CLI: %s\n' "$(gh --version | sed -n '1p')"
printf 'Target architecture: %s\n' "${architecture}"

step "Checking repository state"
[[ $(git branch --show-current) = "${source_branch}" ]] || fail "Run releases from the ${source_branch} branch."
[[ -z $(git status --porcelain) ]] || fail "The working tree is not clean. Commit or stash changes first."
[[ $(git remote get-url "${remote}") == *github.com* ]] || fail "Remote ${remote} is not a GitHub repository."
git rev-parse --verify HEAD >/dev/null
git rev-parse --verify "refs/heads/${release_branch}" >/dev/null || fail "Local ${release_branch} branch does not exist."
git merge-base --is-ancestor "${release_branch}" HEAD || fail "${release_branch} contains commits not present in ${source_branch}."
if git rev-parse --verify "refs/tags/${tag}" >/dev/null 2>&1; then
  fail "Local tag ${tag} already exists."
fi
if grep -Fq "## [${version}]" CHANGELOG.md; then
  fail "CHANGELOG.md already contains version ${version}."
fi
unreleased_entries=$(awk '
  /^## \[Unreleased\]$/ { active=1; next }
  active && /^## \[/ { exit }
  active && /^- / { count++ }
  END { print count+0 }
' CHANGELOG.md)
[[ ${unreleased_entries} -gt 0 ]] || fail "CHANGELOG.md has no entries under Unreleased."
printf 'Unreleased changelog entries: %s\n' "${unreleased_entries}"

if [[ ${dry_run} = false ]]; then
  step "Checking GitHub access and remote branches"
  gh auth status >/dev/null || fail "GitHub CLI is not authenticated. Run: gh auth login"
  git fetch --prune "${remote}"
  git rev-parse --verify "refs/remotes/${remote}/${source_branch}" >/dev/null || fail "Remote branch ${remote}/${source_branch} does not exist."
  [[ $(git rev-parse HEAD) = $(git rev-parse "${remote}/${source_branch}") ]] || fail "Local ${source_branch} must exactly match ${remote}/${source_branch} before release."
  if git rev-parse --verify "refs/remotes/${remote}/${release_branch}" >/dev/null 2>&1; then
    git merge-base --is-ancestor "${remote}/${release_branch}" HEAD || fail "Remote ${release_branch} cannot be fast-forwarded to ${source_branch}."
  fi
  if git ls-remote --exit-code --tags "${remote}" "refs/tags/${tag}" >/dev/null 2>&1; then
    fail "Remote tag ${tag} already exists."
  fi
fi

step "Running verification"
[[ -z $(gofmt -l cmd internal) ]] || fail "Go files are not formatted. Run gofmt before release."
go vet ./...
go test -race ./...
bash -n scripts/*.sh packaging/*.sh packaging/debian/postinst packaging/debian/prerm packaging/debian/postrm
git diff --check

if [[ ${dry_run} = true ]]; then
  step "Building dry-run artifacts"
  LPC_VERSION="${version}" ./scripts/build.sh
  LPC_VERSION="${version}" ./scripts/build-deb.sh
  printf '\nDry run completed. No Git commits, tags, pushes, or GitHub releases were created.\n'
  exit 0
fi

printf '\nRelease plan:\n'
printf '  Version:          %s\n' "${version}"
printf '  Tag:              %s\n' "${tag}"
printf '  Source branch:    %s\n' "${source_branch}"
printf '  Release branch:   %s\n' "${release_branch}"
printf '  GitHub mode:      %s\n' "$([[ ${draft} = true ]] && echo draft || echo published)"
printf '  Debian package:   dist/%s\n' "${deb_name}"
printf '  Binary archive:   dist/%s\n' "${archive_name}"

if [[ ${assume_yes} = false ]]; then
  read -r -p "Type RELEASE ${version} to continue: " confirmation
  [[ ${confirmation} = "RELEASE ${version}" ]] || fail "Release cancelled."
fi

step "Updating changelog and creating release commit"
./scripts/update-changelog.sh release "${version}"
git add CHANGELOG.md
git commit -m "chore(release): ${tag}"

step "Building release artifacts"
LPC_VERSION="${version}" ./scripts/build.sh
LPC_VERSION="${version}" ./scripts/build-deb.sh
mkdir -p dist
tar -czf "dist/${archive_name}" bin/local-parental-control bin/lpctl README.md CHANGELOG.md LICENSE
(
  cd dist
  sha256sum "${deb_name}" "${archive_name}" >"${checksums_name}"
)

awk -v version="${version}" '
  index($0, "## [" version "] - ") == 1 { active=1; next }
  active && /^## \[/ { exit }
  active { print }
' CHANGELOG.md >"${notes_file}"
[[ -s ${notes_file} ]] || fail "Could not extract release notes from CHANGELOG.md."

step "Creating tag and updating local main branch"
git tag -a "${tag}" -m "Local Parental Control ${tag}"
git branch -f "${release_branch}" HEAD

step "Pushing branches and tag atomically"
git push --atomic "${remote}" "${source_branch}" "${release_branch}" "${tag}"

step "Creating GitHub Release"
gh_args=(release create "${tag}" "dist/${deb_name}" "dist/${archive_name}" "dist/${checksums_name}" --title "Local Parental Control ${tag}" --notes-file "${notes_file}" --verify-tag)
if [[ ${draft} = true ]]; then gh_args+=(--draft); fi
gh "${gh_args[@]}"

step "Release completed"
printf 'Version: %s\n' "${version}"
printf 'Commit:  %s\n' "$(git rev-parse --short HEAD)"
printf 'Tag:     %s\n' "${tag}"
printf 'URL:     %s\n' "$(gh release view "${tag}" --json url --jq .url)"
