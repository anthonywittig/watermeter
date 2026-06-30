#!/bin/bash
#
# Renders firestore.rules from firestore.rules.template, injecting the allowed
# emails from the (private) watermeter-config repo. The rendered firestore.rules
# is gitignored so the family's emails never land in this public repo.
#
# Run this before `firebase deploy --only firestore:rules`.

set -e

# start in the repo root (this script lives in dev/)
cd "$(dirname "$0")/.."

EMAILS_FILE="../watermeter-config/config/firebase/allowed-emails.json"
if [[ ! -f "$EMAILS_FILE" ]]; then
    echo "Missing $EMAILS_FILE -- is watermeter-config cloned next to this repo?" >&2
    exit 1
fi

# Build a comma-separated list of quoted emails: "a@x.com", "b@y.com"
EMAILS=$(node -e '
  const fs = require("fs");
  const list = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  process.stdout.write(list.map(e => JSON.stringify(e)).join(", "));
' "$EMAILS_FILE")

node -e '
  const fs = require("fs");
  const tpl = fs.readFileSync("firestore.rules.template", "utf8");
  if (!tpl.includes("__ALLOWED_EMAILS__")) {
    console.error("template is missing the __ALLOWED_EMAILS__ placeholder");
    process.exit(1);
  }
  fs.writeFileSync("firestore.rules", tpl.replace("__ALLOWED_EMAILS__", process.argv[1]));
' "$EMAILS"

echo "Wrote firestore.rules"
