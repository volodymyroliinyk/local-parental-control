#!/usr/bin/env bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
changelog=${CHANGELOG_FILE:-"${project_dir}/CHANGELOG.md"}

usage() {
  cat >&2 <<'EOF'
Usage:
  update-changelog.sh add TYPE MESSAGE
  update-changelog.sh release VERSION [YYYY-MM-DD]

TYPE is one of: added, changed, fixed, removed, security
EOF
  exit 2
}

[[ $# -ge 1 ]] || usage
command_name=$1
shift

if [[ ! -f ${changelog} ]]; then
  echo "Changelog not found: ${changelog}" >&2
  exit 1
fi

temporary=$(mktemp)
trap 'rm -f -- "$temporary"' EXIT

case "${command_name}" in
  add)
    [[ $# -ge 2 ]] || usage
    entry_type=$1
    shift
    message=$*
    case "${entry_type}" in
      added) heading=Added ;;
      changed) heading=Changed ;;
      fixed) heading=Fixed ;;
      removed) heading=Removed ;;
      security) heading=Security ;;
      *) echo "Unknown changelog type: ${entry_type}" >&2; usage ;;
    esac
    [[ -n ${message} ]] || usage

    awk -v heading="${heading}" -v message="${message}" '
      BEGIN { in_unreleased=0; found=0; inserted=0 }
      /^## \[Unreleased\]$/ { in_unreleased=1; print; next }
      in_unreleased && /^## \[/ {
        if (!found) {
          print "### " heading
          print ""
          print "- " message
          print ""
          inserted=1
        }
        in_unreleased=0
      }
      in_unreleased && $0 == "### " heading {
        found=1
        print
        print ""
        print "- " message
        inserted=1
        next
      }
      { print }
      END {
        if (!inserted) exit 3
      }
    ' "${changelog}" >"${temporary}" || {
      echo "Could not find the Unreleased section." >&2
      exit 1
    }
    ;;
  release)
    [[ $# -ge 1 && $# -le 2 ]] || usage
    version=$1
    release_date=${2:-$(date +%F)}
    if [[ ! ${version} =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
      echo "Version must follow Semantic Versioning, for example 1.2.3." >&2
      exit 1
    fi
    if [[ ! ${release_date} =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
      echo "Date must use YYYY-MM-DD format." >&2
      exit 1
    fi
    if grep -Fq "## [${version}]" "${changelog}"; then
      echo "Version ${version} already exists in the changelog." >&2
      exit 1
    fi

    awk -v version="${version}" -v release_date="${release_date}" '
      /^## \[Unreleased\]$/ {
        print
        print ""
        print "## [" version "] - " release_date
        found=1
        next
      }
      { print }
      END { if (!found) exit 3 }
    ' "${changelog}" >"${temporary}" || {
      echo "Could not find the Unreleased section." >&2
      exit 1
    }
    ;;
  *) usage ;;
esac

mv "${temporary}" "${changelog}"
trap - EXIT
echo "Updated ${changelog}"

