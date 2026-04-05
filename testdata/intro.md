# Introduction to qi

qi is a local-first knowledge search tool for macOS.
It indexes your documents and lets you search using full-text and vector search.

## Getting Started

First, run `qi init` to create your configuration and database.
Then add collections to your config file at `~/.config/qi/config.yaml`.

### Collections

A collection is a directory of documents that qi will index.
You can have multiple collections, each with its own path and file extensions.

## Search Modes

qi supports three search modes:

- **lexical**: BM25 full-text search
- **hybrid**: BM25 + vector search with Reciprocal Rank Fusion
- **deep**: hybrid + reranking

## Configuration

The configuration file uses YAML format.
See `config.example.yaml` for a fully annotated example.
