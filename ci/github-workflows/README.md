# GitHub Actions workflows (to be moved into place)

These belong in `.github/workflows/`. The agent that wrote this provider pushes with a GitHub App that cannot
create workflow files, so a maintainer needs to move them:

```sh
git mv ci/github-workflows/test.yml ci/github-workflows/release.yml .github/workflows/
git rm ci/github-workflows/README.md
```

* `test.yml`: build and golangci-lint; `make generate` must leave no diff; acceptance tests on Terraform 1.5, 1.11
  and 1.17; `make e2e`.
* `release.yml`: GoReleaser on `v*` tags, signing with the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets.
