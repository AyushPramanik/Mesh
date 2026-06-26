#!/usr/bin/env bash
#
# A self-contained Mesh demo: two agents work in parallel, a conflict gets
# caught, and their PRs land together in a merge train — no GitHub required.
# This is the script behind docs/QUICKSTART.md; run it from the repo root after
# `make build`.
set -euo pipefail

MESH=./bin/mesh
MESHD=./bin/meshd
SANDBOX="$(mktemp -d)/demo"

say() { printf "\n\033[1;36m▶ %s\033[0m\n" "$1"; }

say "1. Create a sandbox repo with a tiny Go codebase"
git init -q -b main "$SANDBOX"
cd "$SANDBOX"
git config user.email demo@mesh.dev
git config user.name "Mesh Demo"
mkdir -p auth api
cat > auth/auth.go <<'GO'
package auth

func Login(token string) bool { return token != "" }
GO
cat > api/handler.go <<'GO'
package api

func Handle(token string) { _ = token }
GO
git add -A && git commit -q -m "initial"
cd - >/dev/null

say "2. mesh init"
"$MESH" --repo "$SANDBOX" init

say "3. Start the daemon"
"$MESHD" --repo "$SANDBOX" 2>/dev/null &
MPID=$!
trap 'kill -TERM $MPID 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do [ -S "$SANDBOX/.mesh/mesh.sock" ] && break; sleep 0.1; done

say "4. mesh doctor"
"$MESH" --repo "$SANDBOX" doctor

say "5. Spin up two agents, each in its own isolated workspace"
A_OUT=$("$MESH" --repo "$SANDBOX" workspace create --agent agent-alpha)
B_OUT=$("$MESH" --repo "$SANDBOX" workspace create --agent agent-beta)
A_ID=$(echo "$A_OUT" | head -1 | awk '{print $3}'); A_BR=$(echo "$A_OUT" | head -1 | awk '{print $6}')
B_ID=$(echo "$B_OUT" | head -1 | awk '{print $3}'); B_BR=$(echo "$B_OUT" | head -1 | awk '{print $6}')
A_WT=$(dirname "$SANDBOX")/demo-worktrees/$A_ID
B_WT=$(dirname "$SANDBOX")/demo-worktrees/$B_ID
echo "alpha -> $A_BR"
echo "beta  -> $B_BR"

say "6. Each agent edits a DIFFERENT file — but Beta references a symbol Alpha changed"
# Alpha changes the definition of Login; Beta calls Login. Different files, so
# plain git (and file-level checks) see no conflict — this is the silent one.
cat > "$A_WT/auth/auth.go" <<'GO'
package auth

func Login(token string, scope string) bool { return token != "" && scope != "" }
GO
cat > "$B_WT/api/handler.go" <<'GO'
package api

import "demo/auth"

func Handle(token string) { _ = auth.Login(token) }
GO
"$MESH" --repo "$SANDBOX" workspace commit "$A_ID" -m "auth: require scope" >/dev/null
"$MESH" --repo "$SANDBOX" workspace commit "$B_ID" -m "api: call Login"   >/dev/null
echo "files changed by alpha: auth/auth.go"
echo "files changed by beta:  api/handler.go   (disjoint!)"
echo "mesh conflict $A_BR $B_BR:"
"$MESH" --repo "$SANDBOX" conflict "$A_BR" "$B_BR"

say "7. Queue both PRs and look at the merge train"
"$MESH" --repo "$SANDBOX" pr submit --workspace "$A_ID" --branch "$A_BR" --title "auth: require scope" --priority 5
"$MESH" --repo "$SANDBOX" pr submit --workspace "$B_ID" --branch "$B_BR" --title "api: call Login"      --priority 5
"$MESH" --repo "$SANDBOX" pr trains

say "8. Land the train into main — local continuous merge, no GitHub needed"
"$MESH" --repo "$SANDBOX" land

say "9. Result: both features are on main"
git -C "$SANDBOX" log --oneline | head -5

say "Done. State lived in $SANDBOX/.mesh"
