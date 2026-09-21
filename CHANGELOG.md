# Changelog

## [0.2.1](https://github.com/itsmostafa/qi/compare/v0.2.0...v0.2.1) (2026-09-21)


### Features

* **config:** add one glob dialect for path scoping ([d8bd744](https://github.com/itsmostafa/qi/commit/d8bd744111b0c4e43992f21ae5aeebc94be43d20))
* consistent ignore globs and --path query scope ([7d08e99](https://github.com/itsmostafa/qi/commit/7d08e998307cb4bb0e11b302c6c7d32c7bf992d3))
* **search:** add --path scope filter to search and query ([6d8f21f](https://github.com/itsmostafa/qi/commit/6d8f21ff047cb5f1dbdb034d93a367e95607b093))


### Bug Fixes

* **indexer:** apply ignore patterns to files, not just directories ([e1c2a81](https://github.com/itsmostafa/qi/commit/e1c2a81e8a54f79adb366e82bf39c5f1ff5e1072))


### Performance Improvements

* **search:** scan vectors lean and hydrate the top-K ([64d488c](https://github.com/itsmostafa/qi/commit/64d488cf040329dbbd43c705ee98d5d4bda00dbb))
* **search:** scan vectors lean and hydrate the top-K ([b657d78](https://github.com/itsmostafa/qi/commit/b657d78f89927a2881e1baaac97e28204c4b5168))


### Miscellaneous Chores

* release 0.2.1 ([118260e](https://github.com/itsmostafa/qi/commit/118260e094de8532228004d07721b3dfc099548c))

## [0.2.0](https://github.com/itsmostafa/qi/compare/v0.1.0...v0.2.0) (2026-09-09)


### Features

* **chunker:** resolve each chunk to a source line range ([8bf6ddc](https://github.com/itsmostafa/qi/commit/8bf6ddcc762507aa9aa444fb98b8671c8051a383))
* **cmd:** print a runnable retrieval command with every hit ([9ba7247](https://github.com/itsmostafa/qi/commit/9ba72475e5f41d82486273487441ec1331fae98c))
* **db:** record chunk source line ranges ([8bad2d3](https://github.com/itsmostafa/qi/commit/8bad2d3c4cb599e18c021a038a5fb5c5f7aa06f1))
* **indexer:** persist chunk line ranges and repair legacy rows ([bc59b5e](https://github.com/itsmostafa/qi/commit/bc59b5edfa6d9f89d8f2607a49c016f9d674229c))
* **indexer:** persist embedding progress between batches ([2b9bf1d](https://github.com/itsmostafa/qi/commit/2b9bf1db83cedadb37781575d598b6aed4e38318))
* **indexer:** persist embedding progress between batches ([8d4f786](https://github.com/itsmostafa/qi/commit/8d4f786480147f8d25630047daa1a5c4a0182704))
* **parser:** accept created as a frontmatter date alias ([a99e25a](https://github.com/itsmostafa/qi/commit/a99e25a850968567253e691d8396fb3117c8cedb))
* **parser:** map retrieval text back to its raw source ([0fd76b1](https://github.com/itsmostafa/qi/commit/0fd76b178c8cce11e8c6a614e06a13783ecb1556))
* **providers:** classify retryable embedding failures ([97e6cc9](https://github.com/itsmostafa/qi/commit/97e6cc98e087a2b9969de8abf76fda66f99da8c2))
* **search:** make every hit citable with source line ranges ([a06213a](https://github.com/itsmostafa/qi/commit/a06213ab5e4e0d4179564dbdb076b9222a43c0f6))
* **search:** return citable hits and bounded supporting passages ([1050d7f](https://github.com/itsmostafa/qi/commit/1050d7fff7a4f22ff77706cd11ba955703843fd5))


### Bug Fixes

* **get:** resolve ambiguity by distinct hash, not by matched paths ([80fb8fe](https://github.com/itsmostafa/qi/commit/80fb8fe87b9d3bb44a34c71c9fb2d2dd9621f438))
* **index:** date every document so --since and --until match ([cae3ddd](https://github.com/itsmostafa/qi/commit/cae3ddd9d10a2a7400852f3d73368a444ce524a4))
* **index:** date every document so --since and --until match ([473d582](https://github.com/itsmostafa/qi/commit/473d5829fff65429abfa977cf131d31d5893751e))
* **indexer:** stop doomed embedding runs instead of walking every batch ([6438015](https://github.com/itsmostafa/qi/commit/64380150dc969cbd8f3fd3d740c694192eaf5ac7))
* **index:** refresh a fallback date when the file's mtime moves ([f4080dd](https://github.com/itsmostafa/qi/commit/f4080dd3d79282fa0d23c05652cff97fc12fe6bc))

## 0.1.0 (2026-09-04)

Initial release.
