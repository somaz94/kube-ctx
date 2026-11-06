# Changelog

All notable changes to this project will be documented in this file.

## [v0.7.2](https://github.com/somaz94/kube-ctx/compare/v0.7.1...v0.7.2) (2026-11-06)

### Bug Fixes

- sweep only files named like session copies so a stray .yaml keeps history ([48568a9](https://github.com/somaz94/kube-ctx/commit/48568a9622cc3107d347fe7d0d017a2d07c7ad14))
- say a session copy never reaches other terminals, not that nothing reads it ([1805e82](https://github.com/somaz94/kube-ctx/commit/1805e82d1779e8966612c37d581a1817391a7951))
- quote = and % and put -- before a dash-led name in the rename restore line ([e2bd15f](https://github.com/somaz94/kube-ctx/commit/e2bd15fc7054e994437ee0cf2546bc85a4ca5657))
- prefer a context named @x over the stripped name when no alias claims x ([7b384f2](https://github.com/somaz94/kube-ctx/commit/7b384f2020956fcf200b675798c45ca1eb6641d7))
- write through a dangling config symlink and apply filePerm explicitly ([ee75d67](https://github.com/somaz94/kube-ctx/commit/ee75d67c5cd3478c5d0713a0d375362367efb602))
- replace config.yaml atomically so an interrupted save keeps the guards ([75b8b84](https://github.com/somaz94/kube-ctx/commit/75b8b84965dd94e552bbecd88140c9016c5fd64b))
- reach a context whose own name starts with @ when no alias claims it ([804b6ba](https://github.com/somaz94/kube-ctx/commit/804b6ba7f99728b32a55446caad737589c9fad85))
- pick the doctor time-left unit after rounding ([8bdfefc](https://github.com/somaz94/kube-ctx/commit/8bdfefc9e6428bacf0d57dcedd7104195ee519e6))
- print installer messages with printf so backslashes stay literal ([9afaf4b](https://github.com/somaz94/kube-ctx/commit/9afaf4b545ea5b6e2f1ca7176f672e9b41966d7b))
- trim doctor errors by rune rather than by byte ([9c66c2d](https://github.com/somaz94/kube-ctx/commit/9c66c2d723321978addcee0d60deef2e49c84cd9))
- count wide characters as two columns when aligning tables ([7247147](https://github.com/somaz94/kube-ctx/commit/724714744b0131297d067bd42d442ca7d660b3b7))
- record a hooked shell's first switch in its new session's history ([665b04a](https://github.com/somaz94/kube-ctx/commit/665b04a2838e558082297529eea9a7bf764d4a06))
- name the namespace history when ns - has nowhere to go back to ([8281346](https://github.com/somaz94/kube-ctx/commit/828134690444f5e7ddaaf2c68d6702be911f3be1))
- stop prefixing a namespace list failure twice ([222e35f](https://github.com/somaz94/kube-ctx/commit/222e35fdaadf2c878844d3c2379b36a8a5255f4c))
- say nothing expires when only the cert-manager overlay was skipped ([201116e](https://github.com/somaz94/kube-ctx/commit/201116e14a6199fdb2fb6560bf95796304d9407e))
- complete -n with the namespaces of the context the command targets ([03218e6](https://github.com/somaz94/kube-ctx/commit/03218e6557ff3b1219594221fe9e57afdf437c0c))
- stop claiming a hooked terminal's session copy is removed on exit ([dfa6813](https://github.com/somaz94/kube-ctx/commit/dfa681354880a9c02e5a7056d999bd15e646ae22))
- read the first CERTIFICATE block when doctor checks a client certificate ([1799cdc](https://github.com/somaz94/kube-ctx/commit/1799cdc855b01886b3f5573c790e5fd378485543))
- carry exact-name guards over when renaming a context ([5f51002](https://github.com/somaz94/kube-ctx/commit/5f51002f78271ba5ecb80f85ec7dfb6a9898a02f))
- refresh $KUBE_CTX_ACTIVE on every switch in a managed shell ([0651eb3](https://github.com/somaz94/kube-ctx/commit/0651eb3ee4d582ed8a84e0e6bf14f56a961577ff))
- report install failures instead of exiting silently ([cadf984](https://github.com/somaz94/kube-ctx/commit/cadf984bb2e9307c286bdec2d598ceeba9eedbbd))
- find the best fuzzy alignment instead of the greedy one ([f087808](https://github.com/somaz94/kube-ctx/commit/f087808bb949ed2fc3f4d4c5e6910496f7aeb56f))
- consume whole escape sequences and skip invalid bytes in the picker ([6b2c04b](https://github.com/somaz94/kube-ctx/commit/6b2c04b66439442d15f74ccc1bc14b2e5c843443))
- honor XDG_STATE_HOME when there is no home directory ([5af12be](https://github.com/somaz94/kube-ctx/commit/5af12be35441cf2dc8160772fd07c9ee5dfce236))
- exit 130 when the picker is closed or delete is declined ([09df966](https://github.com/somaz94/kube-ctx/commit/09df966092b73363cdd6ba16a884a002b8b14928))
- let a bare name mean the context and @ force the alias ([1dd7b86](https://github.com/somaz94/kube-ctx/commit/1dd7b862b2413244cbda4e21e22a5e978563e993))
- resolve an alias target the way every context argument is ([8481335](https://github.com/somaz94/kube-ctx/commit/8481335e38da1e372ac35b1abe06d1869d42ec83))
- share one prompt reader across export's guarded targets ([1b9cb11](https://github.com/somaz94/kube-ctx/commit/1b9cb11ea403eef5ebb5757f01b4369d6134d495))
- stop offering contexts in the exec --all and -c command slot ([5620328](https://github.com/somaz94/kube-ctx/commit/56203281781aac5076c3569bddedd4a024d85e29))
- escape backslashes and quotes when exporting to fish ([2bcc999](https://github.com/somaz94/kube-ctx/commit/2bcc99958783f8acb35434fe69ce3b880fe52f41))

### Code Refactoring

- drop unreachable namespace defaults from kctx current ([7b2f5cc](https://github.com/somaz94/kube-ctx/commit/7b2f5cccfebdcebfef17e4ca2c46babb076e98fd))

### Documentation

- add related projects section to README ([38c7068](https://github.com/somaz94/kube-ctx/commit/38c7068ba01ac93a94d4b99e03f29311be187e6c))
- add a demo GIF of the picker and production guard ([e723826](https://github.com/somaz94/kube-ctx/commit/e7238266bc4db87e426d5aaa3f857d8faf51413e))
- trim comments in cmd/cli ([9c8798c](https://github.com/somaz94/kube-ctx/commit/9c8798cb25d7d03c9140386b2d99101d205009f3))
- trim comments in the config, guard, contexts, kubeconfig and transfer packages ([b9f9112](https://github.com/somaz94/kube-ctx/commit/b9f91126c7b34b1541b41d2506d1e951ba7de38f))
- trim comments in the picker and shellenv packages ([9fabb7e](https://github.com/somaz94/kube-ctx/commit/9fabb7ea7e531665a5dc5cc136080a58ba900482))
- trim comments in the expiry, probe, namespaces and render packages ([9e51b18](https://github.com/somaz94/kube-ctx/commit/9e51b187d6a49e27d4fa5a133c171606fa704238))
- trim comments in the cmd/cli tests ([479e750](https://github.com/somaz94/kube-ctx/commit/479e750c4cfb45e4ae35765c53f7d858bffb9159))
- trim comments in the e2e script, workflows and Makefile ([6af563a](https://github.com/somaz94/kube-ctx/commit/6af563aae19dffa84bc1e1919a39d040bdcee67f))
- correct the fish hook's note on the start directory ([9d6edbf](https://github.com/somaz94/kube-ctx/commit/9d6edbf4553e6cc07eca2e69c3637830d423622b))
- say kctx ns opens the picker when given no argument ([33b9c28](https://github.com/somaz94/kube-ctx/commit/33b9c28e20435fa6514b1e0ab85450e9a0b76ba2))
- drop table escapes from a code span outside a table ([be4d7b0](https://github.com/somaz94/kube-ctx/commit/be4d7b05af9b9d9427223156e4611db58ba53259))
- correct which test stubs newHarness installs ([b76d0b0](https://github.com/somaz94/kube-ctx/commit/b76d0b0094f618faeee44d974f1035e099a157e3))
- note that clientcmd also drops comments and key order ([b97d540](https://github.com/somaz94/kube-ctx/commit/b97d5407dd5275f0f6739a2fe5217450b9b0c6f1))
- state that kubeconfig merging is first-writer-wins ([2adc6b1](https://github.com/somaz94/kube-ctx/commit/2adc6b19ef471c1086636c6fe56929c106cd313d))
- correct and trim comments in the e2e script, installer and workflows ([8091ed1](https://github.com/somaz94/kube-ctx/commit/8091ed11efadeed619c2c15a5993fb9faee4b4eb))
- correct and trim comments across cmd/cli ([af7724a](https://github.com/somaz94/kube-ctx/commit/af7724afa393ae77a33d0a14417e90bf3e25b690))
- correct and trim comments across pkg ([4fdee2a](https://github.com/somaz94/kube-ctx/commit/4fdee2a8cf3b61990ab44369e8b3586c739335bc))
- name every command a guard gates in the config header and guard help ([7292122](https://github.com/somaz94/kube-ctx/commit/72921223b6b9ed3f878113ccfdf48c2db22ffed7))
- fix the install commands that 404 or fail silently ([e5f8b7a](https://github.com/somaz94/kube-ctx/commit/e5f8b7abc927924859b245fd4687eafc7b736962))
- correct the user guides against the code ([07d9e98](https://github.com/somaz94/kube-ctx/commit/07d9e986ff5afeff3345d9ab09c77ddb1381d623))
- correct CLAUDE.md key concepts against the code ([811b32d](https://github.com/somaz94/kube-ctx/commit/811b32d2136f5689e2f6ee412a34027b116225d1))
- correct stale comments across cmd/cli ([7737049](https://github.com/somaz94/kube-ctx/commit/773704959662376da3e058a108c49a12d4712a59))
- correct stale comments across pkg ([58d71c8](https://github.com/somaz94/kube-ctx/commit/58d71c8d30bd5da22a40654e236a76ec8e8f4fe6))
- correct stale comments in tests, e2e script and workflow ([36a258e](https://github.com/somaz94/kube-ctx/commit/36a258eb85ebbb0f00b30e73e5557c68263c7550))

### Tests

- match check_hook's session copies by ID in the e2e sessions check ([7bd7fc9](https://github.com/somaz94/kube-ctx/commit/7bd7fc9ffcd25e0a0d4288ceb6787bc39de34e69))
- cover export in the -y guard bypass check ([855ef21](https://github.com/somaz94/kube-ctx/commit/855ef219e5fcefa8d8556d4ed3cb35cc923d34e5))
- make the plain-output check able to fail ([012f1e9](https://github.com/somaz94/kube-ctx/commit/012f1e9a0f214aa6ec06f3efa41ca1bdac06df26))
- re-exec the e2e suite under bash when started from zsh ([ed3e614](https://github.com/somaz94/kube-ctx/commit/ed3e614cd192534a5d2b3953e94acf89ff987215))
- clear KUBE_CTX_* variables before the e2e suite runs ([d63e59f](https://github.com/somaz94/kube-ctx/commit/d63e59f46fd951e706a68c15e4a4aa1ab0881927))
- isolate the unit suite from a managed terminal's KUBE_CTX_* variables ([00babcb](https://github.com/somaz94/kube-ctx/commit/00babcb7c297103b0fbf103bfcf120afa3480bf6))
- cover the root -N history shorthand ([7fb5bfe](https://github.com/somaz94/kube-ctx/commit/7fb5bfe6249f43d990d3b419b1d69727f47a59a3))

### Builds

- **deps:** bump the go-mod-minor group with 3 updates (#18) ([#18](https://github.com/somaz94/kube-ctx/pull/18)) ([a7bf681](https://github.com/somaz94/kube-ctx/commit/a7bf68150fc59c2b1d25e1ba4376c260349a2cc0))
- **deps:** bump helm/kind-action (#17) ([#17](https://github.com/somaz94/kube-ctx/pull/17)) ([68dab73](https://github.com/somaz94/kube-ctx/commit/68dab730b808a4cfff1c00be5138491cccc9741c))
- **deps:** bump the go-mod-minor group with 2 updates (#16) ([#16](https://github.com/somaz94/kube-ctx/pull/16)) ([b1fb7d7](https://github.com/somaz94/kube-ctx/commit/b1fb7d708ef9a59bf94d55a02ced9c8ce840e5f3))
- **deps:** bump golang.org/x/text (#15) ([#15](https://github.com/somaz94/kube-ctx/pull/15)) ([6dacba7](https://github.com/somaz94/kube-ctx/commit/6dacba704ea6faa57c35eb85ae0a58985b6e5b51))
- **deps:** bump the go-mod-minor group with 3 updates (#14) ([#14](https://github.com/somaz94/kube-ctx/pull/14)) ([e83ca22](https://github.com/somaz94/kube-ctx/commit/e83ca2222ef8da1ab283551e7c6aa2e23fdce345))
- **deps:** bump golang.org/x/term in the go-mod-minor group (#13) ([#13](https://github.com/somaz94/kube-ctx/pull/13)) ([99362d2](https://github.com/somaz94/kube-ctx/commit/99362d2d50edb333922143733b852c1eee305b5a))
- **deps:** bump the go-mod-minor group with 2 updates (#12) ([#12](https://github.com/somaz94/kube-ctx/pull/12)) ([216a007](https://github.com/somaz94/kube-ctx/commit/216a0075bbbf4773f506fe84ed6d4416d00cd064))
- **deps:** bump golangci/golangci-lint-action from 8 to 9 (#10) ([#10](https://github.com/somaz94/kube-ctx/pull/10)) ([91b1546](https://github.com/somaz94/kube-ctx/commit/91b1546f5a8248ce130f791ccb533adb15ab861e))
- **deps:** bump the go-mod-minor group with 3 updates (#11) ([#11](https://github.com/somaz94/kube-ctx/pull/11)) ([b1a97c5](https://github.com/somaz94/kube-ctx/commit/b1a97c561d10d16e0f9852d56cd752601c10315c))

### Continuous Integration

- install kind through a pinned helm/kind-action ([5bd289e](https://github.com/somaz94/kube-ctx/commit/5bd289e36d171d7bd9c3f9cf1b2bea28888b2a43))
- run unit tests uncached so newly installed shells are exercised ([33965a9](https://github.com/somaz94/kube-ctx/commit/33965a9f2857c345a2dff2301712e7583b2bf612))
- install zsh and fish strictly so the real-shell tests stop skipping ([6d5c180](https://github.com/somaz94/kube-ctx/commit/6d5c1807e35a21557d09ab68a4db1d23873577ae))
- run E2E on a label only when it is ok-to-test ([0a9afcd](https://github.com/somaz94/kube-ctx/commit/0a9afcd692bc8df5fc0c0166c50095f4eff03616))
- trim redundant comments in gitlab-mirror workflow ([610607f](https://github.com/somaz94/kube-ctx/commit/610607f01c1422877347b6a30d3a2fb35f87a6d9))
- retry mirror pushes on transient remote failures ([d28d0f1](https://github.com/somaz94/kube-ctx/commit/d28d0f1cdd9ee40cf3221cbdc6d97bb38e2eff07))
- drop the dead issue-close trigger from changelog generation ([a3f2961](https://github.com/somaz94/kube-ctx/commit/a3f2961c8963ba025864a5eed9ba573af3b6378a))

### Chores

- replace placeholder LICENSE with Apache 2.0 text ([0118a52](https://github.com/somaz94/kube-ctx/commit/0118a52403abce803c89bc96cc697eeb0e894cdd))
- base create-pr.sh on origin/main ([1702fe4](https://github.com/somaz94/kube-ctx/commit/1702fe4f7b4178e809c86264aaea6dd6b9ff5df7))
- classify scoped and breaking commits in create-pr.sh ([2a200e8](https://github.com/somaz94/kube-ctx/commit/2a200e82c5454dec2b002035205327d02d9879e4))
- trim boilerplate comments in repo config and scripts ([64d56f0](https://github.com/somaz94/kube-ctx/commit/64d56f0368bfb66104a9dff64202fc5c3ee150cb))
- add a reviewed find-sensitive allowlist for the changelog author lines ([c54b36d](https://github.com/somaz94/kube-ctx/commit/c54b36d79121ddc5fa1e1e6cc9db33b77d445b9a))

### Contributors

- somaz

<br/>

## [v0.7.1](https://github.com/somaz94/kube-ctx/compare/v0.7.0...v0.7.1) (2026-08-18)

### Bug Fixes

- stop rewriting -N past the argument terminator ([a404adb](https://github.com/somaz94/kube-ctx/commit/a404adb44409dadac9854cb5b3126d2489befb5b))

### Code Refactoring

- put the expiry branches where a test can reach them ([c472f75](https://github.com/somaz94/kube-ctx/commit/c472f7529696638d09451f1e10b47ae8afad4cc0))

### Contributors

- somaz

<br/>

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

