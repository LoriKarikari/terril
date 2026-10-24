# Contributing to wolkpack with an AI agent

This guide is for coding agents preparing a pull request or an issue for wolkpack, and for the people running them. `CONTRIBUTING.md` says which changes are accepted. `AGENTS.md` holds the project rules. This file is the workflow. Run the steps in order. Each one ends in something you can show.

## Steps

### 1. Check that the change is accepted

Read the "What's welcome" section of `CONTRIBUTING.md` and place the change in one of its three groups.

- **Pull request.** Continue to step 2.
- **Issue first.** Search the open issues. If none covers it, open one with the issue form in `.github/ISSUE_TEMPLATE/` and stop. Build nothing until a maintainer agrees on the issue.
- **Usually declined.** Stop. Tell your human why.

**Done when** you can name the group and quote the line from `CONTRIBUTING.md` that puts the change there.

### 2. Reproduce the problem with the real binary

Build wolkpack and run it on an app that shows the problem. Save the exact commands, the output of `wolkpack plan`, the build log, and what the container does when it runs.

A guess from reading the code isn't a reproduction.

**Done when** you have commands and their captured output that show the problem happening.

### 3. Find the root cause

Trace the problem back to the code that produced it. Explain why it happens, not only where. A wrong detection usually starts in `pack/facts.go`. A wrong Dockerfile line usually starts in a provider.

**Done when** you can state the cause in one or two sentences, with the file and function.

### 4. Make the smallest change that fixes it

Change only what the cause requires. Leave unrelated cleanups, renames and refactors out. If you find a second problem, note it for a separate issue.

Check the change against every rule in `AGENTS.md`. The ones agents break most often are these:

- Providers read typed facts. Reading an app file outside `pack/facts.go` is wrong.
- When two answers are equally likely, wolkpack fails with an error that names both.
- Images run as UID 1000 and contain only what the app needs at runtime.
- Any change to CLI flags or `wolkpack.toml` is a contract change. That needs an issue first, which means going back to step 1.

**Done when** the diff touches only what step 3 named, and every `AGENTS.md` rule still holds.

### 5. Prove it end to end

Add a test app to `e2e/testdata/` that reproduces the problem, as described in the testing section of `CONTRIBUTING.md`. Run `just test <app>` before your change and after it, and capture both results. Don't write unit tests.

**Done when** the test app fails without your change and passes with it, and `just lint` and `just test` pass.

### 6. Write the pull request

Fill in every section of `.github/pull_request_template.md`. The title and every commit follow the commit message rules in `CONTRIBUTING.md`. Show the whole description to your human before submitting. They review the diff and own the submission.

**Done when** every section of the template is filled in with real output, and your human has read the diff.
