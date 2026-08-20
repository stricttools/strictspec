# Reversibility gap: `migrate` is up-only

## Context

strictcli is gaining declared reversibility support: a mutating command will
declare which command undoes it (verified at registration in both
directions), a command with no recovery will declare irreversible with a
mandatory reason, a warn-severity check will flag destructive commands
declaring neither, and after a real run the framework will print a paste-able
recovery command and emit a machine-readable recovery member in the JSON
result document. When this repo adopts that support, the gap below needs
either a built inverse or an honest irreversible declaration.

## Problem

`migrate` migrates documents up to the current format version with no
down-migration. A migration that should not have run is recovered by hand
(or via version control, since the documents are typically committed).

## Effort

Small if declared irreversible with a version-control reason; medium if
down-migrations become real (each format-version step would need a declared
inverse transform).
