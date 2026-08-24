# Changelog

## [0.4.0](https://github.com/todor-a/lazybrew/compare/v0.3.0...v0.4.0) (2026-08-24)


### Features

* fill the version column from brew list --versions ([#22](https://github.com/todor-a/lazybrew/issues/22)) ([0054dbc](https://github.com/todor-a/lazybrew/commit/0054dbc7a83ab1ec528f189e50dd7aaff42f1619))
* filter the list with is: qualifiers in search ([#33](https://github.com/todor-a/lazybrew/issues/33)) ([b066699](https://github.com/todor-a/lazybrew/commit/b06669908869d2b1fb4ccdebcbbffa603d13b278))
* keep the list browsable while an operation runs ([#20](https://github.com/todor-a/lazybrew/issues/20)) ([96be172](https://github.com/todor-a/lazybrew/commit/96be172a49ca7ec7a4f659b5004ef13e15ae6adb))
* lead the footer with the action and give it two tones ([#10](https://github.com/todor-a/lazybrew/issues/10)) ([7ad558c](https://github.com/todor-a/lazybrew/commit/7ad558c57e6823e015e1bbd4f2cd60ed67257844))
* mark packages from untrusted taps ([#21](https://github.com/todor-a/lazybrew/issues/21)) ([7c2eb37](https://github.com/todor-a/lazybrew/commit/7c2eb37f5ff04377c5ec5c9555bcf03a7b2f36b4))
* name the version transition on queue entries ([#34](https://github.com/todor-a/lazybrew/issues/34)) ([2fcf939](https://github.com/todor-a/lazybrew/commit/2fcf939de5bc457213f5c4f957d2f8c84770da50))
* queue operations and run them serially ([#26](https://github.com/todor-a/lazybrew/issues/26)) ([026c78c](https://github.com/todor-a/lazybrew/commit/026c78c963516da79ab2de5d8e988458fbbe55c5))
* render the list as a table with headings and a sort cue ([#23](https://github.com/todor-a/lazybrew/issues/23)) ([6c9abea](https://github.com/todor-a/lazybrew/commit/6c9abeab18aedc798e316715390ab7cce645280d))
* restyle the UI, adapt themes to the terminal, and persist settings ([#12](https://github.com/todor-a/lazybrew/issues/12)) ([d503a4a](https://github.com/todor-a/lazybrew/commit/d503a4a8658f29e3046a68de4f04689b8743ce4c))
* seed startup from an on-disk snapshot and log to a file ([#13](https://github.com/todor-a/lazybrew/issues/13)) ([eaf226f](https://github.com/todor-a/lazybrew/commit/eaf226f4e900d040b312f0ed9acb1765a877ba58))
* show installed and latest versions for outdated packages ([#14](https://github.com/todor-a/lazybrew/issues/14)) ([d189ad0](https://github.com/todor-a/lazybrew/commit/d189ad07e5691ebc9da0cb81ea192df8eb5a1ff1))
* tab-complete is: qualifiers in search ([#37](https://github.com/todor-a/lazybrew/issues/37)) ([f4b0327](https://github.com/todor-a/lazybrew/commit/f4b032757e93e43348a2dac1eb764458838faa8c))
* threshold the outdated mark by version distance and highlight the bump ([#27](https://github.com/todor-a/lazybrew/issues/27)) ([a1d5d24](https://github.com/todor-a/lazybrew/commit/a1d5d2401e5cd0e603e1eacae59398f023a02030))
* upgrade the selected package, and rebind uninstall to d ([#11](https://github.com/todor-a/lazybrew/issues/11)) ([d2a4f56](https://github.com/todor-a/lazybrew/commit/d2a4f560825c6cda4d4cfeef9e31b3c962e26047))


### Fixes

* close the four recorded review minors ([#8](https://github.com/todor-a/lazybrew/issues/8)) ([b9d1103](https://github.com/todor-a/lazybrew/commit/b9d11035efa5a2dba7fbe1c5435de58cc1a4b880))
* detect the askpass helper by path so brew's env scrub cannot strip it ([#36](https://github.com/todor-a/lazybrew/issues/36)) ([b5437eb](https://github.com/todor-a/lazybrew/commit/b5437ebf0dec472debe1ef8b3a2c0fabcd89f2bb))
* keep the queue overlay on empty selections and log job lifecycles ([#32](https://github.com/todor-a/lazybrew/issues/32)) ([baa3727](https://github.com/todor-a/lazybrew/commit/baa3727783420e2487eb46f40e1cb645272f00cc))
* log askpass peer rejections so silent auth failures name their check ([#38](https://github.com/todor-a/lazybrew/issues/38)) ([35161ed](https://github.com/todor-a/lazybrew/commit/35161ed925ee0ab20834d32d5fee173071564206))


### Documentation

* add hierarchical AGENTS.md documentation ([#30](https://github.com/todor-a/lazybrew/issues/30)) ([42de440](https://github.com/todor-a/lazybrew/commit/42de440c6ceff80a8c4dacc371a7ca78796fddad))
* give the README a demo, badges, and an honest feature tour ([#29](https://github.com/todor-a/lazybrew/issues/29)) ([6672e85](https://github.com/todor-a/lazybrew/commit/6672e85373915b501b483ba7ac3948598a0d06fc))

## [0.3.0](https://github.com/todor-a/lazybrew/compare/v0.2.2...v0.3.0) (2026-08-24)


### Features

* mark outdated packages from brew outdated ([#4](https://github.com/todor-a/lazybrew/issues/4)) ([755943d](https://github.com/todor-a/lazybrew/commit/755943d65aa015647a4540295206f29fe59e870a))
* show the whole fleet with sizes, dependency origin, and a size sort ([#5](https://github.com/todor-a/lazybrew/pull/5)) ([755943d](https://github.com/todor-a/lazybrew/commit/755943d65aa015647a4540295206f29fe59e870a))


### Build and release

* authenticate release-please with a personal access token ([#3](https://github.com/todor-a/lazybrew/issues/3)) ([709b58f](https://github.com/todor-a/lazybrew/commit/709b58fa1093498ca82bb65146662ea5f53efcda))
* require a Conventional Commit pull request title ([00b9314](https://github.com/todor-a/lazybrew/commit/00b93141b36b8e8d892fd036f90240385f60f791))

## [0.2.2](https://github.com/todor-a/lazybrew/compare/v0.2.1...v0.2.2) (2026-08-21)


### Build and release

* adopt release-please for versioning and changelogs ([b4cdc65](https://github.com/todor-a/lazybrew/commit/b4cdc65aac9dca525826feef12c5c1d7cab34a16))
* generate release changelogs from commits ([7fd28f2](https://github.com/todor-a/lazybrew/commit/7fd28f2023a901e709ab80cec303b78130f33e15))
