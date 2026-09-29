# Syncing with upstream

This repository is a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang). It was imported by merging upstream commit `21f1a2e` with `--allow-unrelated-histories`, so upstream history and authorship are kept.

## Pull later upstream fixes

```sh
git remote add upstream https://github.com/hackers365/xiaozhi-esp32-server-golang.git   # once
git fetch upstream
git merge upstream/main
```

Never squash and never copy files in by hand. A squash loses the merge base, and every later sync then conflicts on everything.

## rerere

`rerere` records how each conflict was resolved and replays it on the next merge. Turn it on once per clone:

```sh
git config rerere.enabled true
```

## Conflicts on removed paths

This fork deletes parts of upstream (`manager/`, voice clone, the redis and manager config providers, and so on). When upstream edits a path this fork removed, git reports a modify/delete conflict.

Rule: keep the deletion.

```sh
git rm <path>
```

Do this for every modify/delete conflict on a path the spec lists under "Removed" (#8). Review any other conflict by hand. If upstream's change touches behaviour this fork kept, port it into the surviving code.

The Go module name stays `xiaozhi-esp32-server-golang`, so upstream imports merge without edits.
