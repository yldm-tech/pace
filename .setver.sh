#!/bin/bash
# Set every workspace package.json that is version-locked to the workspace release train.
set -euo pipefail
cd /Users/e-hu/Workspace/pace
V="$1"
for f in package.json apps/admin/package.json apps/space/package.json apps/web/package.json \
         packages/codemods/package.json packages/constants/package.json packages/editor/package.json \
         packages/hooks/package.json packages/i18n/package.json packages/propel/package.json \
         packages/services/package.json packages/shared-state/package.json \
         packages/tailwind-config/package.json packages/types/package.json \
         packages/typescript-config/package.json packages/ui/package.json packages/utils/package.json; do
  [ -f "$f" ] || continue
  node -e '
    const fs=require("fs"); const [f,v]=process.argv.slice(1);
    let s=fs.readFileSync(f,"utf8");
    const out=s.replace(/^(\s*"version":\s*")[^"]+(",?)$/m, `$1${v}$2`);
    if(!/^\s*"version":\s*"/m.test(s)) { console.error("no version field in "+f); process.exit(1); }
    fs.writeFileSync(f,out);
  ' "$f" "$V"
done
echo "all package.json set to $V"
