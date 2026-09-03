# Changelog

## [0.10.0](https://github.com/itsmostafa/qi/compare/v0.9.1...v0.10.0) (2026-09-03)


### ⚠ BREAKING CHANGES

* **install:** `brew install qi` is no longer supported. Existing Homebrew users should run `brew uninstall qi` and reinstall with the install script.

### Features

* **install:** add install.sh for one-line binary installation ([04bb3a3](https://github.com/itsmostafa/qi/commit/04bb3a36ac6dc00856047b97929fc0fccfaff5bf))
* **install:** remove Homebrew support ([10c7ec1](https://github.com/itsmostafa/qi/commit/10c7ec126e97774274078e6d4482976c14cbc68a))
* **install:** replace Homebrew with an install.sh script ([0eb5b93](https://github.com/itsmostafa/qi/commit/0eb5b9377aac789fb04ab56cbd103d5734513ecd))


### Bug Fixes

* **update:** point at sudo when the install directory is not writable ([b4a2695](https://github.com/itsmostafa/qi/commit/b4a269587c291749eb33d76be09b187e915c2b93))

## [0.9.1](https://github.com/itsmostafa/qi/compare/v0.9.0...v0.9.1) (2026-09-03)


### Bug Fixes

* harden indexing and embedding integrity ([8d4f4a9](https://github.com/itsmostafa/qi/commit/8d4f4a9aa13038d7065fa3fe8435b9d46d74b06e))

## [0.9.0](https://github.com/itsmostafa/qi/compare/v0.8.0...v0.9.0) (2026-06-04)


### Features

* **config:** expand env vars in provider api_key and base_url fields ([7d9bf4f](https://github.com/itsmostafa/qi/commit/7d9bf4fb2c422ac47b35b1f1e741ea0a8108402c))
* **providers:** env var expansion in config and base_url normalization ([0e01f25](https://github.com/itsmostafa/qi/commit/0e01f253d9812ac8c21d000329680a30d3614aaf))
* **providers:** truncate oversized embedding inputs at max_input_chars ([230830c](https://github.com/itsmostafa/qi/commit/230830c6711be21eda7a1fd5a6f7b2664bb6f1e7))
* **search:** add extension-based score boosting ([5590f3f](https://github.com/itsmostafa/qi/commit/5590f3f1a68e39772de93abedc19537e84e8c555))


### Bug Fixes

* **chunker:** force-split lines exceeding target size ([afad394](https://github.com/itsmostafa/qi/commit/afad394c281d153f8544f87dad508497c09401d6))
* **config:** expand env vars in rerank provider base_url ([4e2fb29](https://github.com/itsmostafa/qi/commit/4e2fb292dde2ad3e8268ed30c5ff2b037d2a1fcc))
* **providers:** normalize base_url to prevent doubled /v1 path ([814688a](https://github.com/itsmostafa/qi/commit/814688a52a3ad589ea29702bbd9e956c8118db18))

## [0.8.0](https://github.com/itsmostafa/qi/compare/v0.7.0...v0.8.0) (2026-05-06)


### Features

* **output:** add ask result formatter with JSON support ([3f68286](https://github.com/itsmostafa/qi/commit/3f68286f5b1722beb8d852c085188acd6f51d984))
* **output:** add ask result formatter with JSON support ([d35ced9](https://github.com/itsmostafa/qi/commit/d35ced909d2ea0eaa2fb6c9c3f3e3e4a2bd0c0a7))
* **output:** track all chunk indices per source and add markdown format ([4fe81c5](https://github.com/itsmostafa/qi/commit/4fe81c506956cd686072b2edf77f3f6e3870fb5e))
* **search:** switch system prompt citations from qi:// URIs to numeric markers ([a022665](https://github.com/itsmostafa/qi/commit/a0226657a05f3349b288c0285d07c9a1033d906c))


### Bug Fixes

* **formula:** correct homebrew test to use --version flag ([c3d1741](https://github.com/itsmostafa/qi/commit/c3d17413adaca0a1b9d5685df9dbbeef5bd4d580))

## [0.7.0](https://github.com/itsmostafa/qi/compare/v0.6.0...v0.7.0) (2026-05-04)


### Features

* **app:** normalize legacy collection names on startup ([1cb7691](https://github.com/itsmostafa/qi/commit/1cb76910f97ff03c631c6cb9848c3d2b2ffd7154))
* **cmd/index:** embed chunks after indexing when embedder is configured ([321f9dc](https://github.com/itsmostafa/qi/commit/321f9dc7071deb7db8b54ed1216d393425fe76b2))
* **cmd/index:** remove --name flag and use auto-generated names ([8d3eb1b](https://github.com/itsmostafa/qi/commit/8d3eb1b4c105395c98a12d1b215b134eea759637))
* **collections:** auto-generate collection names from path segments ([f49da60](https://github.com/itsmostafa/qi/commit/f49da6062ae4ba4771e7942039e4a8d2a8629020))
* **config:** derive collection names from full path segments ([4bbc2cd](https://github.com/itsmostafa/qi/commit/4bbc2cdbcb50d6402001c6814cca2db447f91af5))
* **db:** add RenameCollectionData for migrating legacy collection names ([b7c74c1](https://github.com/itsmostafa/qi/commit/b7c74c1caa4aa283c5173efa4f13834e51d04df8))
* **search:** add query relaxation for natural-language BM25 queries ([221beeb](https://github.com/itsmostafa/qi/commit/221beebf83ab9a8c2a47753811d55c6a1fc35166))


### Bug Fixes

* **cmd/delete:** resolve collection by generated or original name ([46f6273](https://github.com/itsmostafa/qi/commit/46f627320c63a03c1fc025e26b9280e664cd3b72))
* **config:** reject collection slug collisions ([199cd82](https://github.com/itsmostafa/qi/commit/199cd82d53e435b235beef3a866cb420fe6efeea))
* **config:** restrict config file permissions to owner-only ([6e1ed59](https://github.com/itsmostafa/qi/commit/6e1ed59d3241da9f84675a1159b737181d6b1552))
* **config:** restrict config file permissions to owner-only ([fa9d3ca](https://github.com/itsmostafa/qi/commit/fa9d3ca5ddd068f1f4a87b7943cb2b8fbe10e244))
* **delete:** prefer exact collection name ([d07c655](https://github.com/itsmostafa/qi/commit/d07c6555206651b0f0d28cff840fed0036d0a69b))

## [0.6.0](https://github.com/itsmostafa/qi/compare/v0.5.1...v0.6.0) (2026-05-03)


### Features

* **indexer:** skip all dot-directories during walk ([36a17ca](https://github.com/itsmostafa/qi/commit/36a17ca643387773025713c6f501884a4ec9fcc0))


### Bug Fixes

* **db:** add ON DELETE CASCADE to chunk_vectors and embeddings ([07a05e8](https://github.com/itsmostafa/qi/commit/07a05e87e9940544e357dffea23efe8a35a7e616))
* **db:** handle missing dimension column in legacy embeddings table ([6021475](https://github.com/itsmostafa/qi/commit/6021475092ccbfd8c79f2c5fcdad090e79c7e879))
* **db:** make migration 003 idempotent with DROP IF EXISTS guards ([3c63343](https://github.com/itsmostafa/qi/commit/3c63343abdb6b76fc35d4b477159d70e2385bdaa))
* **db:** preserve embedding dimension during migration 003 table rebuild ([1cc2607](https://github.com/itsmostafa/qi/commit/1cc260747fea131a671cc52ab12b332102168956))
* **indexer:** preserve embeddings when reactivating unchanged documents ([40c2376](https://github.com/itsmostafa/qi/commit/40c23765a076d44de4fca87d601315d6a5354456))
* **indexer:** reactivate deactivated documents when file is restored ([9b2a1dc](https://github.com/itsmostafa/qi/commit/9b2a1dc4d7e8408aa7d6792853037b9d4935167b))
* **indexer:** reactivate deactivated documents when file is restored ([aa4b02d](https://github.com/itsmostafa/qi/commit/aa4b02d535d782c2664a17942d02ef7f29455d29))
* **indexer:** return error on non-ErrNoRows scan failure in indexFile ([d58e3d6](https://github.com/itsmostafa/qi/commit/d58e3d6559bc2461b5b098add1b88ca034e5c31b))

## [0.5.1](https://github.com/itsmostafa/qi/compare/v0.5.0...v0.5.1) (2026-04-06)


### Bug Fixes

* **indexer:** skip common VCS/tool/build dirs by default ([be089b0](https://github.com/itsmostafa/qi/commit/be089b0d07870dad101e703f2e461c90eee3a5f2))
* **indexer:** skip common VCS/tool/build dirs by default ([3d1a649](https://github.com/itsmostafa/qi/commit/3d1a649d25feca37b2b442a2b00466c29d0f93bd))
* **marketplace:** use ./ instead of . for plugin source path ([d765c04](https://github.com/itsmostafa/qi/commit/d765c04ff684fc7266294de36d2f9dd002debf50))
* **marketplace:** use ./ instead of . for plugin source path ([e826a16](https://github.com/itsmostafa/qi/commit/e826a16338afd8baa084374ca6895795f875278d))

## [0.5.0](https://github.com/itsmostafa/qi/compare/v0.4.0...v0.5.0) (2026-04-05)


### Features

* auto-named collections and configuration reference ([7960559](https://github.com/itsmostafa/qi/commit/7960559d0ce011cc8fdd56d4dd17704fb2d45cc5))
* **config:** add SlugFromPath for auto-generating collection names ([b80345d](https://github.com/itsmostafa/qi/commit/b80345dcc69050bee4c10d0a09723883bf6406ce))


### Bug Fixes

* **cmd:** rename existing collection when --name targets same path ([61d3600](https://github.com/itsmostafa/qi/commit/61d3600a905915ac8233214f330a118c23c7a077))
* **config:** eliminate data-loss window in collection rename flow ([e66bb75](https://github.com/itsmostafa/qi/commit/e66bb75a56d8aafeb79227f793356b8292f34e55))
* **index:** preserve collection settings on --name rename ([66a8056](https://github.com/itsmostafa/qi/commit/66a80567c6a3d4ad69f4a6d5fa6b5c88cafcc33a))
* **testdata:** update config example path reference in intro.md ([3511a34](https://github.com/itsmostafa/qi/commit/3511a34aa54add3dc9c9ea8cc2d62a64eee414d1))

## [0.4.0](https://github.com/itsmostafa/qi/compare/v0.3.0...v0.4.0) (2026-04-05)


### Features

* **cmd:** add list and delete collection commands ([d1e8ff1](https://github.com/itsmostafa/qi/commit/d1e8ff1c5e374fb5149398cd1bfdfb1e326e00cb))
* **config:** add APIKey to EmbeddingProviderConfig and applyEnvOverrides for openai preset ([c1e20ba](https://github.com/itsmostafa/qi/commit/c1e20ba78af210248222fd19523548b70f2cbec1))
* **index:** add --name flag to save directories as named collections ([6a40091](https://github.com/itsmostafa/qi/commit/6a4009128db77d813a3d4c99305ca90ec5a720aa))
* **providers:** send Authorization header in embedding provider when api_key is set ([a0317d6](https://github.com/itsmostafa/qi/commit/a0317d6c24fabf36ac157047114538fbe4845d7e))


### Bug Fixes

* **db:** add migration to create chunk_vectors and embeddings tables ([461bea6](https://github.com/itsmostafa/qi/commit/461bea64d14e113f305f27245de607fba2c0e5cd))
* **search:** strip punctuation and stop words from FTS5 queries ([59ee4ab](https://github.com/itsmostafa/qi/commit/59ee4abf9cc0688194a7c95769214ec4267b371b))
* **update:** match tar.gz release assets and detect Homebrew installs ([35154c8](https://github.com/itsmostafa/qi/commit/35154c8900653370098be20e6f7008f493817e0f))

## [0.3.0](https://github.com/itsmostafa/qi/compare/v0.2.0...v0.3.0) (2026-04-05)


### ⚠ BREAKING CHANGES

* rename project from goralph to qi, remove old loop code
* **cli:** The 'build' and 'plan' subcommands have been removed. Use 'goralph run' instead.

### Features

* add Go application scaffolding ([bc5f2a1](https://github.com/itsmostafa/qi/commit/bc5f2a16f938989c7bc1a7c97b031bb2341f261b))
* **cli:** add --no-push flag to skip pushing after iterations ([1b6b549](https://github.com/itsmostafa/qi/commit/1b6b549c6b162e26fb2e8ee74bd2ba23d4923e4f))
* **cli:** add --no-push flag to skip pushing after iterations ([5cfd41f](https://github.com/itsmostafa/qi/commit/5cfd41f8d091fefa436e54bb771135d30e980398))
* **cli:** add CLI provider type and --cli flag ([90dce37](https://github.com/itsmostafa/qi/commit/90dce371198e373feea70d132b3bfde714053e2c))
* **cli:** add cobra CLI with build and plan commands ([0f4d17e](https://github.com/itsmostafa/qi/commit/0f4d17ecc77a2cec711c61fdc3862f30dcbc7dfc))
* **cli:** add OpenAI Codex CLI support ([e591048](https://github.com/itsmostafa/qi/commit/e5910486ed066ad8907538b12a640aa1be02f45d))
* **cmd:** add CLI flags for RLM mode and verification ([4f5ecf2](https://github.com/itsmostafa/qi/commit/4f5ecf223d5079dcda4653e06d1f369222528677))
* **cmd:** add self-update command ([94fbc0c](https://github.com/itsmostafa/qi/commit/94fbc0ca3ede848dd1aa4de1cb23b6194dc23af1))
* **cmd:** generate unique plan file per session ([87435ca](https://github.com/itsmostafa/qi/commit/87435ca5a419c0fdd115e907562a649c8c206cf8))
* **cmd:** replace --rlm flag with --mode flag ([9d91b47](https://github.com/itsmostafa/qi/commit/9d91b475c59a7e3cf85d79e857973e3ff1131e8c))
* **cmd:** support indexing current directory and arbitrary paths ([36f25e3](https://github.com/itsmostafa/qi/commit/36f25e346fd4af47f29277ed910cf234c4482cbf))
* **codex:** implement Codex CLI output parsing ([d2e2069](https://github.com/itsmostafa/qi/commit/d2e2069bc01357499cf84606fea8165e91cc8fe6))
* **core:** implement qi local-first knowledge search CLI ([4fd5628](https://github.com/itsmostafa/qi/commit/4fd562888377bf6b99c83031f36ee9378cdca1bf))
* **loop:** add agentic loop script for Ralph Wiggum technique ([82d2397](https://github.com/itsmostafa/qi/commit/82d2397979e8df81b5047005c21bad5b2eb74e25))
* **loop:** add completion promise detection for early loop termination ([b0dfdc7](https://github.com/itsmostafa/qi/commit/b0dfdc7779551ec109f4e449d3bca0b1a99fc1c1))
* **loop:** add completion promise detection for early loop termination ([6538782](https://github.com/itsmostafa/qi/commit/65387820c27a809493590ae30298f49439e10c36))
* **loop:** add implementation plan support with Codex CLI integration plan ([5f2dad0](https://github.com/itsmostafa/qi/commit/5f2dad0719583f1b352bb9f3dd1b60ff2bc620e2))
* **loop:** add iteration-aware task generation guidance ([939affa](https://github.com/itsmostafa/qi/commit/939affa6aaa85e428a76635a4535b06aaad085a5))
* **loop:** add iteration-aware task generation guidance ([4b16afd](https://github.com/itsmostafa/qi/commit/4b16afdfe6c90c98de1b67a989800151305bc48b))
* **loop:** add JSON message types for Claude output parsing ([413a64e](https://github.com/itsmostafa/qi/commit/413a64e149d416e64c374fccff64581e5bdcc449))
* **loop:** add JSON parsing and iteration logging ([2721aee](https://github.com/itsmostafa/qi/commit/2721aeebad2db3278709728beed2734c5dc2609a))
* **loop:** add Mode type and ModeRunner interface ([63e960d](https://github.com/itsmostafa/qi/commit/63e960db0bf7cbc7ea24347f29aae794d00e650a))
* **loop:** add PhaseRouter for RLM phase inference and guidance ([53f922e](https://github.com/itsmostafa/qi/commit/53f922ed5ecccf183ef07204d9bd2773e700d858))
* **loop:** add Provider interface for CLI abstraction ([b6883bd](https://github.com/itsmostafa/qi/commit/b6883bd3b435d72b0d2959e2cae1fc4293620d1a))
* **loop:** add RalphRunner implementing ModeRunner ([f559c53](https://github.com/itsmostafa/qi/commit/f559c5317f9dbf2facccb8149ba7ce5f4ec8f5fd))
* **loop:** add real-time streaming output with tool status indicators ([66e5148](https://github.com/itsmostafa/qi/commit/66e514845811ac19bcc25071987faf7948c228ef))
* **loop:** add RLM mode with structured phase-based execution ([1734469](https://github.com/itsmostafa/qi/commit/1734469151e3221859f5936257131682c5071e4d))
* **loop:** add RLM output formatting ([9b07d25](https://github.com/itsmostafa/qi/commit/9b07d25a336ba17b3f61fd08338c23cf5a44a80f))
* **loop:** add RLM prompt builder with context injection ([ef97dcf](https://github.com/itsmostafa/qi/commit/ef97dcf0ae4507340dd9e9c61cb4a268456480ff))
* **loop:** add RLM type definitions and constants ([21804f4](https://github.com/itsmostafa/qi/commit/21804f4eb839c4724f2d216fb0d92eb9eadec2f4))
* **loop:** add RLMRunner implementing ModeRunner ([564cac7](https://github.com/itsmostafa/qi/commit/564cac79385c6bef64529f19b1f33aa9ab210cc1))
* **loop:** add session-scoped implementation plan files ([08bd675](https://github.com/itsmostafa/qi/commit/08bd675fe01b6530434015f85f3923c48935e557))
* **loop:** add session-scoped plan path generation ([01218c4](https://github.com/itsmostafa/qi/commit/01218c44c3d62ef743053ee45920c28fc375c595))
* **loop:** add StateManager for RLM state persistence ([24ce92e](https://github.com/itsmostafa/qi/commit/24ce92e8085bf4a78540c995ff987d3fdd009817))
* **loop:** add styled output formatting with lipgloss ([7f1f78a](https://github.com/itsmostafa/qi/commit/7f1f78a71ea0af0f219d6e495fb89ad8e4b72d1b))
* **loop:** add support for providers without cost/duration data ([326f024](https://github.com/itsmostafa/qi/commit/326f024e7ef2145fb6f521237a60f3134d4c74be))
* **loop:** add Verifier for build/test validation ([33196b7](https://github.com/itsmostafa/qi/commit/33196b7202887c221e8fe717e8259c76eff60e41))
* **loop:** detect RLM markers in agent output ([975a755](https://github.com/itsmostafa/qi/commit/975a755580fe9f273b8b318edf2960071dc05271))
* **loop:** display model in header for Claude and Codex providers ([9ad405b](https://github.com/itsmostafa/qi/commit/9ad405bb413631733b387c81fb1539a15886b3df))
* **loop:** extend Config with RLM and verification options ([149564c](https://github.com/itsmostafa/qi/commit/149564cddf4edafb89be24b8eba63525e8cec69e))
* **loop:** integrate RLM mode and verification into main loop ([1a33b1e](https://github.com/itsmostafa/qi/commit/1a33b1e616bdbceca685bf7a68959cd152a6c871))
* **loop:** introduce ModeRunner interface for extensible execution modes ([989bb18](https://github.com/itsmostafa/qi/commit/989bb18f098ca78f49ebaa36c6d4910cd2d7ee67))
* **loop:** make --no-push skip commits in addition to pushes ([5a2cac0](https://github.com/itsmostafa/qi/commit/5a2cac00b3f4b5783030cd3198f03d1c518572b6))
* **loop:** use session-scoped plan files in loop execution ([4ce5144](https://github.com/itsmostafa/qi/commit/4ce5144d6dcb28bf284be7b7a7a3b92719e329ff))
* **output:** update tool completion indicators in-place using ANSI cursor control ([bfca20d](https://github.com/itsmostafa/qi/commit/bfca20d229a8da9511a8aa48bd1f410dd92fc9f0))
* **rlm:** add JSON schemas for agent-written state files ([fcb9aa6](https://github.com/itsmostafa/qi/commit/fcb9aa65d53d93574063a6826c07b0d50dfc7ebc))
* **runner:** add Runner interface for AI CLI abstraction ([77e2496](https://github.com/itsmostafa/qi/commit/77e249699bfebfc38cbaac64826e8123d0e0d5d8))
* **taskfile:** add install task for ~/.local/bin ([87d7d7e](https://github.com/itsmostafa/qi/commit/87d7d7e5012ca9f591908e8d0f774d8da253a481))
* **version:** add version package with ldflags support ([6742732](https://github.com/itsmostafa/qi/commit/6742732e596dee010452e7a0d4a8915fedeee36e))


### Bug Fixes

* **loop:** count reasoning items as turns for Codex provider ([616f8f3](https://github.com/itsmostafa/qi/commit/616f8f39f275582ae6fdf0bbeabeab5c57d972fb))
* **loop:** reset text tracking after tool results to prevent truncation ([8f580db](https://github.com/itsmostafa/qi/commit/8f580dbcdc52cd1b39a1e95c8b2b6ef3283847db))
* **loop:** run --verify without requiring RLM marker ([04c8255](https://github.com/itsmostafa/qi/commit/04c8255cb7a14ba875ca2551753cf4c301fafcbb))
* **loop:** start first iteration in PLAN phase ([7ca2052](https://github.com/itsmostafa/qi/commit/7ca2052208e9dfff42aab36d6df165c3115699b3))
* **loop:** use millisecond precision in plan path to prevent collisions ([35451d7](https://github.com/itsmostafa/qi/commit/35451d77a28301ec9687d4b5edcb0296db185ff4))
* **loop:** use zero-padded iteration in state filenames ([35926bc](https://github.com/itsmostafa/qi/commit/35926bc88babb0cd36844a8f3bfa468165bb3ed2))
* move bump-minor-pre-major inside package config ([d8e1d68](https://github.com/itsmostafa/qi/commit/d8e1d683165f63bcdd712560883e3161ae07b629))
* **output:** improve newline handling between text and tool indicators ([2ba5eb8](https://github.com/itsmostafa/qi/commit/2ba5eb85cdf6d48a3272d67bb6d4fac4d81ae2ce))
* **output:** improve newline handling between text and tool indicators ([ce823c6](https://github.com/itsmostafa/qi/commit/ce823c6523a6fef4979cfa46d861d9ec1079e43d))
* prevent breaking changes from bumping to 1.0.0 ([21ef1d6](https://github.com/itsmostafa/qi/commit/21ef1d6be1dace2c0f2f6c29c946264ed979a29e))
* **rlm:** ensure agent implements during ACT phase ([4a9b57c](https://github.com/itsmostafa/qi/commit/4a9b57c45ae33b28e7d7c2535dec0a0f111d1976))
* **rlm:** explicitly instruct agent to implement changes in no-push mode ([2e35a73](https://github.com/itsmostafa/qi/commit/2e35a739c2f4594ca793f49368727c0470dd6f7a))


### Miscellaneous Chores

* rename project from goralph to qi, remove old loop code ([5df734e](https://github.com/itsmostafa/qi/commit/5df734e48e2265a98c113413b41fb66ccc93b33a))


### Code Refactoring

* **cli:** replace build/plan commands with unified run command ([80a561a](https://github.com/itsmostafa/qi/commit/80a561ab46f3e339609fd9dd8a31dd1b466ba842))

## [0.2.0](https://github.com/itsmostafa/qi/compare/qi-v0.1.0...qi-v0.2.0) (2026-04-05)


### ⚠ BREAKING CHANGES

* rename project from goralph to qi, remove old loop code
* **cli:** The 'build' and 'plan' subcommands have been removed. Use 'goralph run' instead.

### Features

* add Go application scaffolding ([bc5f2a1](https://github.com/itsmostafa/qi/commit/bc5f2a16f938989c7bc1a7c97b031bb2341f261b))
* **cli:** add --no-push flag to skip pushing after iterations ([1b6b549](https://github.com/itsmostafa/qi/commit/1b6b549c6b162e26fb2e8ee74bd2ba23d4923e4f))
* **cli:** add --no-push flag to skip pushing after iterations ([5cfd41f](https://github.com/itsmostafa/qi/commit/5cfd41f8d091fefa436e54bb771135d30e980398))
* **cli:** add CLI provider type and --cli flag ([90dce37](https://github.com/itsmostafa/qi/commit/90dce371198e373feea70d132b3bfde714053e2c))
* **cli:** add cobra CLI with build and plan commands ([0f4d17e](https://github.com/itsmostafa/qi/commit/0f4d17ecc77a2cec711c61fdc3862f30dcbc7dfc))
* **cli:** add OpenAI Codex CLI support ([e591048](https://github.com/itsmostafa/qi/commit/e5910486ed066ad8907538b12a640aa1be02f45d))
* **cmd:** add CLI flags for RLM mode and verification ([4f5ecf2](https://github.com/itsmostafa/qi/commit/4f5ecf223d5079dcda4653e06d1f369222528677))
* **cmd:** generate unique plan file per session ([87435ca](https://github.com/itsmostafa/qi/commit/87435ca5a419c0fdd115e907562a649c8c206cf8))
* **cmd:** replace --rlm flag with --mode flag ([9d91b47](https://github.com/itsmostafa/qi/commit/9d91b475c59a7e3cf85d79e857973e3ff1131e8c))
* **cmd:** support indexing current directory and arbitrary paths ([36f25e3](https://github.com/itsmostafa/qi/commit/36f25e346fd4af47f29277ed910cf234c4482cbf))
* **codex:** implement Codex CLI output parsing ([d2e2069](https://github.com/itsmostafa/qi/commit/d2e2069bc01357499cf84606fea8165e91cc8fe6))
* **core:** implement qi local-first knowledge search CLI ([4fd5628](https://github.com/itsmostafa/qi/commit/4fd562888377bf6b99c83031f36ee9378cdca1bf))
* **loop:** add agentic loop script for Ralph Wiggum technique ([82d2397](https://github.com/itsmostafa/qi/commit/82d2397979e8df81b5047005c21bad5b2eb74e25))
* **loop:** add completion promise detection for early loop termination ([b0dfdc7](https://github.com/itsmostafa/qi/commit/b0dfdc7779551ec109f4e449d3bca0b1a99fc1c1))
* **loop:** add completion promise detection for early loop termination ([6538782](https://github.com/itsmostafa/qi/commit/65387820c27a809493590ae30298f49439e10c36))
* **loop:** add implementation plan support with Codex CLI integration plan ([5f2dad0](https://github.com/itsmostafa/qi/commit/5f2dad0719583f1b352bb9f3dd1b60ff2bc620e2))
* **loop:** add iteration-aware task generation guidance ([939affa](https://github.com/itsmostafa/qi/commit/939affa6aaa85e428a76635a4535b06aaad085a5))
* **loop:** add iteration-aware task generation guidance ([4b16afd](https://github.com/itsmostafa/qi/commit/4b16afdfe6c90c98de1b67a989800151305bc48b))
* **loop:** add JSON message types for Claude output parsing ([413a64e](https://github.com/itsmostafa/qi/commit/413a64e149d416e64c374fccff64581e5bdcc449))
* **loop:** add JSON parsing and iteration logging ([2721aee](https://github.com/itsmostafa/qi/commit/2721aeebad2db3278709728beed2734c5dc2609a))
* **loop:** add Mode type and ModeRunner interface ([63e960d](https://github.com/itsmostafa/qi/commit/63e960db0bf7cbc7ea24347f29aae794d00e650a))
* **loop:** add PhaseRouter for RLM phase inference and guidance ([53f922e](https://github.com/itsmostafa/qi/commit/53f922ed5ecccf183ef07204d9bd2773e700d858))
* **loop:** add Provider interface for CLI abstraction ([b6883bd](https://github.com/itsmostafa/qi/commit/b6883bd3b435d72b0d2959e2cae1fc4293620d1a))
* **loop:** add RalphRunner implementing ModeRunner ([f559c53](https://github.com/itsmostafa/qi/commit/f559c5317f9dbf2facccb8149ba7ce5f4ec8f5fd))
* **loop:** add real-time streaming output with tool status indicators ([66e5148](https://github.com/itsmostafa/qi/commit/66e514845811ac19bcc25071987faf7948c228ef))
* **loop:** add RLM mode with structured phase-based execution ([1734469](https://github.com/itsmostafa/qi/commit/1734469151e3221859f5936257131682c5071e4d))
* **loop:** add RLM output formatting ([9b07d25](https://github.com/itsmostafa/qi/commit/9b07d25a336ba17b3f61fd08338c23cf5a44a80f))
* **loop:** add RLM prompt builder with context injection ([ef97dcf](https://github.com/itsmostafa/qi/commit/ef97dcf0ae4507340dd9e9c61cb4a268456480ff))
* **loop:** add RLM type definitions and constants ([21804f4](https://github.com/itsmostafa/qi/commit/21804f4eb839c4724f2d216fb0d92eb9eadec2f4))
* **loop:** add RLMRunner implementing ModeRunner ([564cac7](https://github.com/itsmostafa/qi/commit/564cac79385c6bef64529f19b1f33aa9ab210cc1))
* **loop:** add session-scoped implementation plan files ([08bd675](https://github.com/itsmostafa/qi/commit/08bd675fe01b6530434015f85f3923c48935e557))
* **loop:** add session-scoped plan path generation ([01218c4](https://github.com/itsmostafa/qi/commit/01218c44c3d62ef743053ee45920c28fc375c595))
* **loop:** add StateManager for RLM state persistence ([24ce92e](https://github.com/itsmostafa/qi/commit/24ce92e8085bf4a78540c995ff987d3fdd009817))
* **loop:** add styled output formatting with lipgloss ([7f1f78a](https://github.com/itsmostafa/qi/commit/7f1f78a71ea0af0f219d6e495fb89ad8e4b72d1b))
* **loop:** add support for providers without cost/duration data ([326f024](https://github.com/itsmostafa/qi/commit/326f024e7ef2145fb6f521237a60f3134d4c74be))
* **loop:** add Verifier for build/test validation ([33196b7](https://github.com/itsmostafa/qi/commit/33196b7202887c221e8fe717e8259c76eff60e41))
* **loop:** detect RLM markers in agent output ([975a755](https://github.com/itsmostafa/qi/commit/975a755580fe9f273b8b318edf2960071dc05271))
* **loop:** display model in header for Claude and Codex providers ([9ad405b](https://github.com/itsmostafa/qi/commit/9ad405bb413631733b387c81fb1539a15886b3df))
* **loop:** extend Config with RLM and verification options ([149564c](https://github.com/itsmostafa/qi/commit/149564cddf4edafb89be24b8eba63525e8cec69e))
* **loop:** integrate RLM mode and verification into main loop ([1a33b1e](https://github.com/itsmostafa/qi/commit/1a33b1e616bdbceca685bf7a68959cd152a6c871))
* **loop:** introduce ModeRunner interface for extensible execution modes ([989bb18](https://github.com/itsmostafa/qi/commit/989bb18f098ca78f49ebaa36c6d4910cd2d7ee67))
* **loop:** make --no-push skip commits in addition to pushes ([5a2cac0](https://github.com/itsmostafa/qi/commit/5a2cac00b3f4b5783030cd3198f03d1c518572b6))
* **loop:** use session-scoped plan files in loop execution ([4ce5144](https://github.com/itsmostafa/qi/commit/4ce5144d6dcb28bf284be7b7a7a3b92719e329ff))
* **output:** update tool completion indicators in-place using ANSI cursor control ([bfca20d](https://github.com/itsmostafa/qi/commit/bfca20d229a8da9511a8aa48bd1f410dd92fc9f0))
* **rlm:** add JSON schemas for agent-written state files ([fcb9aa6](https://github.com/itsmostafa/qi/commit/fcb9aa65d53d93574063a6826c07b0d50dfc7ebc))
* **runner:** add Runner interface for AI CLI abstraction ([77e2496](https://github.com/itsmostafa/qi/commit/77e249699bfebfc38cbaac64826e8123d0e0d5d8))
* **taskfile:** add install task for ~/.local/bin ([87d7d7e](https://github.com/itsmostafa/qi/commit/87d7d7e5012ca9f591908e8d0f774d8da253a481))
* **version:** add version package with ldflags support ([6742732](https://github.com/itsmostafa/qi/commit/6742732e596dee010452e7a0d4a8915fedeee36e))


### Bug Fixes

* **loop:** count reasoning items as turns for Codex provider ([616f8f3](https://github.com/itsmostafa/qi/commit/616f8f39f275582ae6fdf0bbeabeab5c57d972fb))
* **loop:** reset text tracking after tool results to prevent truncation ([8f580db](https://github.com/itsmostafa/qi/commit/8f580dbcdc52cd1b39a1e95c8b2b6ef3283847db))
* **loop:** run --verify without requiring RLM marker ([04c8255](https://github.com/itsmostafa/qi/commit/04c8255cb7a14ba875ca2551753cf4c301fafcbb))
* **loop:** start first iteration in PLAN phase ([7ca2052](https://github.com/itsmostafa/qi/commit/7ca2052208e9dfff42aab36d6df165c3115699b3))
* **loop:** use millisecond precision in plan path to prevent collisions ([35451d7](https://github.com/itsmostafa/qi/commit/35451d77a28301ec9687d4b5edcb0296db185ff4))
* **loop:** use zero-padded iteration in state filenames ([35926bc](https://github.com/itsmostafa/qi/commit/35926bc88babb0cd36844a8f3bfa468165bb3ed2))
* move bump-minor-pre-major inside package config ([d8e1d68](https://github.com/itsmostafa/qi/commit/d8e1d683165f63bcdd712560883e3161ae07b629))
* **output:** improve newline handling between text and tool indicators ([2ba5eb8](https://github.com/itsmostafa/qi/commit/2ba5eb85cdf6d48a3272d67bb6d4fac4d81ae2ce))
* **output:** improve newline handling between text and tool indicators ([ce823c6](https://github.com/itsmostafa/qi/commit/ce823c6523a6fef4979cfa46d861d9ec1079e43d))
* prevent breaking changes from bumping to 1.0.0 ([21ef1d6](https://github.com/itsmostafa/qi/commit/21ef1d6be1dace2c0f2f6c29c946264ed979a29e))
* **rlm:** ensure agent implements during ACT phase ([4a9b57c](https://github.com/itsmostafa/qi/commit/4a9b57c45ae33b28e7d7c2535dec0a0f111d1976))
* **rlm:** explicitly instruct agent to implement changes in no-push mode ([2e35a73](https://github.com/itsmostafa/qi/commit/2e35a739c2f4594ca793f49368727c0470dd6f7a))


### Miscellaneous Chores

* rename project from goralph to qi, remove old loop code ([5df734e](https://github.com/itsmostafa/qi/commit/5df734e48e2265a98c113413b41fb66ccc93b33a))


### Code Refactoring

* **cli:** replace build/plan commands with unified run command ([80a561a](https://github.com/itsmostafa/qi/commit/80a561ab46f3e339609fd9dd8a31dd1b466ba842))

## Changelog
