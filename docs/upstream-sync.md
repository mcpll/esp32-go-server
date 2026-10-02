# Syncing with upstream

This repository is a fork of [hackers365/xiaozhi-esp32-server-golang](https://github.com/hackers365/xiaozhi-esp32-server-golang). Upstream was first brought in at commit `21f1a2e`. That import landed on `main` as a squash (`2604b07`), which dropped the merge base. The base was restored afterwards by merging branch `import-upstream` with `-s ours` (`Record upstream 21f1a2e as merge base`), so the tree of `main` stayed unchanged and `21f1a2e` became an ancestor again.

## First-sync check

Before every merge from upstream:

```sh
git fetch upstream
git merge-base main upstream/main
```

That must print `21f1a2e71ff383723f1464ea9b137016e6feab8d`, or a later upstream commit once a real sync has landed. If it prints nothing, stop: the histories are unrelated again and a merge would conflict on every shared path (add/add). Do not merge until the base is restored.

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
