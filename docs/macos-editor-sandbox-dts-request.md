# Apple DTS request — draft for manual submission

**Historical record:** native prototype source and demo tools have been removed.
References to their paths/behavior below are evidence from the earlier review,
not current implementation or runnable acceptance instructions.

**Status: parked, not submitted.** The user changed course to the
[embedded-engine evaluation](embedded-editor-evaluation.md); this draft is
historical, not an active submission task. If separately resumed, submit manually
outside pi through [Apple Developer technical support](https://developer.apple.com/support/technical/).
Do not include credentials, private notes, keys, user configuration or transcripts.
This is an architecture/supportability question, not a reproducible OS bug report.

## Subject

Supported macOS confinement for a non-persisting editor helper

## Request body

We are evaluating a private-text editor helper in a macOS application. The
parent owns encrypted storage and all encryption keys. The helper receives only
text over parent-owned anonymous pipes; it has no key or storage API. We need an
OS-enforced boundary rather than trusting editor options or application code.

Before receiving any private text, the helper must be prevented from:

- Persisting text anywhere in the filesystem, including its own application
  container, temporary directories, logs, preferences and memory-mapped files.
- Creating additional processes or replacing its executable to escape the
  intended helper lifetime and identity.
- Opening unauthorized network or local IPC routes, or asking another service
  to persist text on its behalf, including Mach/XPC and Unix socket services.
- Using unrelated inherited writable descriptors, connected sockets, mappings
  or Mach rights as persistence/proxy capabilities.

The intended communication is limited to the parent's owned pipe endpoints.
The parent treats helper output as untrusted data and performs encrypted
publication itself. A null diagnostic sink is acceptable; a writable file or
writable directory is not. We exclude OS-managed paging/hibernation/core dumps,
screen/terminal capture and a compromised OS from this application-level goal.

Apple's App Sandbox documentation states that an app has full read/write access
to its container and can run programs there. Omitting optional file-access
entitlements therefore does not appear sufficient. Signed helper sandbox
inheritance appears to preserve that permission. The documented alternatives
`sandbox_init` and `sandbox-exec` are deprecated; we do not want to base a new
security guarantee on unsupported interfaces or undocumented custom profiles.

Could you clarify:

1. Is this complete boundary achievable for a native command-line helper using
   supported macOS APIs and deployment mechanisms? If so, which APIs and minimum
   macOS/SDK versions support it, and what signing/packaging requirements apply?
2. Can supported configuration remove all container/temp write access and deny
   new process creation/executable replacement, rather than just constrain where
   a helper may write or let descendants inherit its sandbox?
3. What supported mechanism restricts inherited descriptors and Mach rights,
   service/proxy access and capability transfer to the intended parent pipes?
4. How should a trusted launcher establish that the boundary applies to the
   exact live helper and pipe endpoints before sending private text? Is entry-time
   enforcement required, or is supported externally verifiable late activation
   possible, accounting for capabilities acquired during harmless startup?
5. If the complete boundary is not supported for a native helper, please confirm
   that limitation. A writable-container exception does not meet our requirement.

One prospective adapter is embedded Neovim 0.11.5, but the confinement design
must be independent of editor-specific Lua/RPC/configuration. Its current
integration explicitly creates a startup Unix socket; that integration is not
claimed compatible with the required policy. We would adapt or reject it rather
than grant writable-directory or listener exceptions.

No protected backend is enabled. We have not selected an exact deployment build
or performed native confinement trials. We would first select a supported target
based on your guidance, then validate only disposable synthetic data with both
negative escape tests and positive editing/encrypted-save/recovery controls.

Relevant documentation:

- https://developer.apple.com/documentation/xcode/configuring-the-macos-app-sandbox
- https://developer.apple.com/documentation/security/accessing-files-from-the-macos-app-sandbox
- https://developer.apple.com/documentation/xcode/embedding-a-helper-tool-in-a-sandboxed-app

## Optional submission fields — fill manually if requested

- Intended macOS deployment target and exact candidate OS build: not selected.
- Candidate Xcode/SDK version: not selected.
- Intended distribution/signing model: not selected.
- Architecture: initial interest is Apple silicon; no approved support matrix.

Do not substitute guessed host values. No local inspection is needed to ask the
supportability question. If support requires a concrete candidate, choose and
supply it manually before any native trial.

## After a reply

Provide only the relevant technical guidance that you are permitted to share,
with credentials, account identifiers and unrelated personal data removed. A
reply proposing an API is a feasibility input, not runtime enforcement proof.
Review support status, exact target/build and all requirements before backend
implementation; keep protected admission unavailable in the meantime.

Background: [feasibility stop and conditional proof matrix](macos-editor-sandbox-feasibility.md).
