# Changelog

All notable changes to this project will be documented in this file.

## [v0.7.0](https://github.com/somaz94/kube-ctx/compare/v0.6.0...v0.7.0) (2026-08-18)

### Features

- report the certificates about to expire with kctx expiry ([ff09114](https://github.com/somaz94/kube-ctx/commit/ff09114a812378e9409650098addaa7f2d041f31))
- switch on a bare context name, the way kubectx takes it ([1540257](https://github.com/somaz94/kube-ctx/commit/15402572e6866ec8146f0c78892a1a6a2eff1733))

### Bug Fixes

- put the expiry gate in a typed field, not a display string ([f08d7ab](https://github.com/somaz94/kube-ctx/commit/f08d7abff7123821e5b12997ac2029c8728fe0a0))
- treat a refused secrets list as unknown, not a partial answer ([fc3538b](https://github.com/somaz94/kube-ctx/commit/fc3538bb0c10ccff74a6b3dc256506ddbd9d10d6))
- exit non-zero when expiry could not read a cluster ([6bd633e](https://github.com/somaz94/kube-ctx/commit/6bd633e2ea3ffc3af21b03d345ecfb1d71e67a95))

### Contributors

- somaz

<br/>

## [v0.6.0](https://github.com/somaz94/kube-ctx/compare/v0.5.0...v0.6.0) (2026-08-18)

### Features

- guard the namespaces inside a context, not just the context ([b2f132f](https://github.com/somaz94/kube-ctx/commit/b2f132f8f4b7aae1df9143b0edacbe8b2cf34002))

### Bug Fixes

- share one buffered reader so a second prompt is not read as a decline ([924302d](https://github.com/somaz94/kube-ctx/commit/924302d1843825525acf14b4e8f074c5e1ca51d3))

### Performance Improvements

- compile the guard rules once per command instead of per lookup ([0b4c71b](https://github.com/somaz94/kube-ctx/commit/0b4c71b14f5ebfa265ee31849bcb972734891d19))

### Documentation

- mention the namespace guard axis in the comparison table ([a63580c](https://github.com/somaz94/kube-ctx/commit/a63580c6caa71a33e39bbf3a059998f122450f30))

### Contributors

- somaz

<br/>

## [v0.5.0](https://github.com/somaz94/kube-ctx/compare/v0.4.0...v0.5.0) (2026-08-14)

### Features

- add kctx sessions and keep a live session from being swept ([58ac5b8](https://github.com/somaz94/kube-ctx/commit/58ac5b8087a0a85401327587d5324ade18c047d6))
- bind a directory to a context and switch on entering it ([817f0c9](https://github.com/somaz94/kube-ctx/commit/817f0c98741c3bc36ce468c219478e940a344ebc))
- run one command against many contexts with kctx exec --all ([ec918ee](https://github.com/somaz94/kube-ctx/commit/ec918ee159271be2d77a998381fd06ec1f132d74))

### Contributors

- somaz

<br/>

## [v0.4.0](https://github.com/somaz94/kube-ctx/compare/v0.3.0...v0.4.0) (2026-08-14)

### Features

- report and prune the stanzas kctx import leaves unreferenced ([d906bc8](https://github.com/somaz94/kube-ctx/commit/d906bc8b553366f569153452553a318243d087a7))
- add kctx import and export, disambiguating colliding stanzas ([7eaf5e0](https://github.com/somaz94/kube-ctx/commit/7eaf5e09996ae4074456b421e641aa5947695cca))

### Contributors

- somaz

<br/>

## [v0.3.0](https://github.com/somaz94/kube-ctx/compare/v0.2.0...v0.3.0) (2026-08-14)

### Features

- publish kctx as the ctx2 kubectl plugin via krew ([b15ac1a](https://github.com/somaz94/kube-ctx/commit/b15ac1ab240bb13fcb865511a8974e835bd1ba1f))

### Bug Fixes

- name the shell hook after the invocation, not the resolved binary ([7a3d07b](https://github.com/somaz94/kube-ctx/commit/7a3d07b845ec470c9b9e074a7656f7effa797080))

### Tests

- add a kind-based e2e suite for the shell hook, doctor and guards ([40d1052](https://github.com/somaz94/kube-ctx/commit/40d10527556c62a3e0a6923a76b6b4cf289584c6))

### Chores

- publish the Homebrew package as a cask instead of a deprecated formula ([8f448d7](https://github.com/somaz94/kube-ctx/commit/8f448d7a91f0148b0ec7e07a99db848848249f18))

### Contributors

- somaz

<br/>

## [v0.2.0](https://github.com/somaz94/kube-ctx/compare/v0.1.0...v0.2.0) (2026-08-14)

### Features

- point at the prompt variables when entering a managed shell ([65a375e](https://github.com/somaz94/kube-ctx/commit/65a375ec0a05769d628777d4ff902cca5b42f983))
- add kctx current, JSON output for alias and guard, and missing completions ([9375192](https://github.com/somaz94/kube-ctx/commit/93751927b8f0de5b2c1ec3fea03d7fa57525b2ec))
- classify contexts by exact name, prefix or suffix and manage rules with kctx guard ([eabb80e](https://github.com/somaz94/kube-ctx/commit/eabb80e48ded616cadb906571b9ae7170fc3a712))

### Bug Fixes

- guard every route to a cluster and make -o and exit codes contractual ([6412277](https://github.com/somaz94/kube-ctx/commit/6412277f5fa435f65e755951eb6eb36d69ea0ba3))
- honor INSTALL_DIR and chmod as the user that moved the binary ([6952925](https://github.com/somaz94/kube-ctx/commit/6952925c04b5fd06113421f33470ff42a4c8a133))

### Code Refactoring

- route context names, history refs and output through one path ([e28ad1c](https://github.com/somaz94/kube-ctx/commit/e28ad1c2c7bbf31494cc9ab8fbdfe5daa12e0107))

### Documentation

- document current, the widened guard, exit codes and the output formats ([5eca6b1](https://github.com/somaz94/kube-ctx/commit/5eca6b1d2f41bba959d21ee6764e829f8cda0026))
- surface the confirm opt-in in ctx help and the guard guides ([7ab0d3d](https://github.com/somaz94/kube-ctx/commit/7ab0d3dd6e0c48c7b82a3440a65943272f454bd0))

### Contributors

- somaz

<br/>

## [v0.1.0](https://github.com/somaz94/kube-ctx/releases/tag/v0.1.0) (2026-08-14)

### Features

- add per-terminal context isolation via subshell, exec and shell hooks ([40a6c1e](https://github.com/somaz94/kube-ctx/commit/40a6c1e80319aa40faf515351d47096f7bdf014c))
- add production guards and doctor health check ([b0e708a](https://github.com/somaz94/kube-ctx/commit/b0e708af47447ae912ce3674a7c3e984d5f2fb8e))
- add built-in fuzzy picker for context and namespace selection ([1bc397b](https://github.com/somaz94/kube-ctx/commit/1bc397b3e4f0e704ebb491b3e1dc203ff31e08f3))
- add context and namespace switching with history, aliases and backups ([ea79af1](https://github.com/somaz94/kube-ctx/commit/ea79af14e2df5c8ca01d75f8d60258f7a2a70338))

### Bug Fixes

- stop leaking session files, losing exit codes and ignoring cancellation ([07171ac](https://github.com/somaz94/kube-ctx/commit/07171ac08902fac426f2151eedcfc1fc7e0d1dec))
- write shell exports in the calling shell's syntax and refuse throwaway edits ([d53c8bf](https://github.com/somaz94/kube-ctx/commit/d53c8bf913519273a34771feac75269dd9402a1c))

### Documentation

- add README, usage, configuration, examples and use-case guides ([5800e33](https://github.com/somaz94/kube-ctx/commit/5800e33db7a77cac3d61e8ad7fe4b020caed8ae4))

### Builds

- **deps:** bump actions/github-script from 8 to 9 (#3) ([#3](https://github.com/somaz94/kube-ctx/pull/3)) ([cac7eb0](https://github.com/somaz94/kube-ctx/commit/cac7eb07bada51d5dc985454385097991e0ca4f7))
- **deps:** bump actions/stale from 10 to 11 (#6) ([#6](https://github.com/somaz94/kube-ctx/pull/6)) ([c929ef0](https://github.com/somaz94/kube-ctx/commit/c929ef0a16e971255b0dcf7505bdb803477b1fca))
- **deps:** bump dependabot/fetch-metadata from 2 to 3 (#2) ([#2](https://github.com/somaz94/kube-ctx/pull/2)) ([44316fc](https://github.com/somaz94/kube-ctx/commit/44316fc3dadbb4ca7ef043ff9dad4207b73aebf3))
- **deps:** bump actions/setup-go from 6 to 7 (#4) ([#4](https://github.com/somaz94/kube-ctx/pull/4)) ([9ee5449](https://github.com/somaz94/kube-ctx/commit/9ee5449393a2619fe92b90960c2c1e4f34356d50))
- **deps:** bump actions/checkout from 6 to 7 (#5) ([#5](https://github.com/somaz94/kube-ctx/pull/5)) ([dc95bf5](https://github.com/somaz94/kube-ctx/commit/dc95bf58a3b14e895b17ff082ec1dec4489cbbb0))
- **deps:** bump golang.org/x/term in the go-mod-minor group (#1) ([#1](https://github.com/somaz94/kube-ctx/pull/1)) ([ee08a06](https://github.com/somaz94/kube-ctx/commit/ee08a06b3d7fe99bcd66bcdda61143add80429be))

### Continuous Integration

- enforce golangci-lint and adopt the shared community workflows ([17b9c0b](https://github.com/somaz94/kube-ctx/commit/17b9c0b9751db6e1ddf11510fd8f2b5b96e922f2))
- add goreleaser release pipeline with homebrew tap and scoop bucket ([52ce53d](https://github.com/somaz94/kube-ctx/commit/52ce53d17c12b20938058b4403a9e10be2e98791))

### Contributors

- somaz

<br/>

