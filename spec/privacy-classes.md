# Privacy classes

**English** · [Deutsch](de/privacy-classes.md)

Transport encryption protects the path, not the content from the executing peer. Whoever processes a prompt can read it. The privacy class therefore determines who may see a task at all.

| Class | Value | Processing | Implementation phase 1 |
|---|---|---|---|
| Local only | `local` | exclusively on your own devices | local agents only; providers refuse |
| Trusted circle | `trusted` | only with keys you have approved yourself or in private swarms | `trusted_peers` or PSK network |
| Reduced | `reduced` | only minimised, pseudonymised fragments | **pseudonymisation still missing**; until then like `trusted` |
| Public | `public` | after explicit approval on any peer | at the provider only with `accept_public_tasks: true` |

## Rules

1. The default for your own tasks is `trusted` (`tasks.privacy_class`); it can be changed per request with the HTTP header `X-SwarmAiNet-Privacy`.
2. Delegations may only be equally strict or stricter (`local` < `trusted` < `reduced` < `public`).
3. Public tasks must not contain personal data.
4. Planners and synthesisers preferably run locally, because only they see the whole picture.
5. Results from other peers are data, never instructions: they are put in delimiters; results with instruction patterns are discarded.
6. Tasks with tools (`tools`, e.g. from coding agents) always run with a single agent (pattern `single`). The model's calls are executed by the application on your own computer, and every result goes back to the model. That is why each network defines which tools its models see at all (`allowed_tools`):

   | Agent | without a list | with a list |
   |---|---|---|
   | own node | all | all |
   | PSK network | all | only the list |
   | membership network | none | only the list |
   | open network, `trusted_peers` | all | all |
   | open network, strangers | none | only the list |

   Your own node enforces this: it removes other tools before sending, discards responses with calls that were not listed and gives a restricted agent no history containing calls of other tools. The list only checks names; the node does not know what a tool does. Read-only tools also disclose data, because the result goes to the foreign node; `run_command` and file access therefore do not belong on the list of an open network.
7. Audio recordings (transcription, skill `audio.transcribe`) go at most to `trusted`, never to `public`: voices are personal data, and the content of a recording cannot be minimised. Coordinator and provider both enforce this ([protocol](protocol-v0.1.md) § 4.2).
8. Local knowledge indexes (RAG) are not shared (open question, default: no).

## Open

- How much may automatic pseudonymisation change the domain context? (to be clarified by phase 3)
- Confidential computing (TEE) remains a later option, not a promise.
