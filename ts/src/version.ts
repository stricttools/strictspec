// The strictspec release this runtime was built at.
//
// Read DIRECTLY from package.json -- the single file rlsbl bumps on release (npm
// target). Because the runtime reads the SAME source the release tool writes,
// version drift is impossible: there is no second hand-maintained constant to
// fall out of sync. Generated code pairs on the generated-code format, not on
// this string, which it carries as information. The JSON import is inlined at
// build/bundle time (browser-safe), and package.json always ships in the npm
// tarball, so `../package.json` also resolves at runtime under Node. A test
// asserts VERSION equals the package.json version as an executable invariant.

import pkg from "../package.json" with { type: "json" };

export const VERSION: string = pkg.version;
