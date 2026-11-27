# Contributing to terril with an AI agent

This guide is for coding agents preparing an issue for terril, and for the people running them. terril takes no pull requests. `CONTRIBUTING.md` says which changes are accepted. `AGENTS.md` holds the project rules. This file is the workflow. Run the steps in order. Each one ends in something you can show.

## Steps

### 1. Check that the change is accepted

Read the "What's welcome" section of `CONTRIBUTING.md` and place the change in one of its two groups.

- **Open an issue.** Search the open issues. If one covers it, add your reproduction there instead. Otherwise continue to step 2.
- **Usually declined.** Stop. Tell your human why.

**Done when** you can name the group and quote the line from `CONTRIBUTING.md` that puts the change there.

### 2. Reproduce the problem with the real binary

Build terril and run it on an app that shows the problem. Save the exact commands, the output of `terril plan`, the build log, and what the container does when it runs.

A guess from reading the code isn't a reproduction.

**Done when** you have commands and their captured output that show the problem happening.

### 3. Find the root cause

Trace the problem back to the code that produced it. Explain why it happens, not only where. A wrong detection usually starts in `pack/facts.go`. A wrong Dockerfile line usually starts in a provider.

**Done when** you can state the cause in one or two sentences, with the file and function.

### 4. Write the issue

Use the issue form in `.github/ISSUE_TEMPLATE/` that fits. Put in the commands and output from step 2 and the cause from step 3. If you built a test app in `e2e/testdata/` that fails, paste it and the `just test <app>` result. Show the whole issue to your human before submitting. They own the submission.

**Done when** every field of the form holds real output, and your human has read it.
