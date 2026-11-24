# Privacy classes

**English** · [Deutsch](de/privacy-classes.md)

Transport encryption protects the path, not the content from the executing peer. Whoever processes a prompt can read it. The privacy class therefore determines who may see a task at all.

| Class | Value | Processing | Implementation phase 1 |
|---|---|---|---|
| Local only | `local` | exclusively on your own devices | local agents only; providers refuse |
| Trusted circle | `trusted` | only with keys you have approved yourself or in private swarms | `trusted_peers` or PSK network |
| Reduced | `reduced` | only minimised, pseudonymised fragments | own and trusted agents see the original, foreign agents only a shortened task with placeholders (rule 9); providers accept it from strangers like `public` |
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
7. Audio recordings (transcription, skill `audio.transcribe`) go at most to `trusted`, never to `public`: voices are personal data, and the content of a recording cannot be minimised. Not as `reduced` either, because a recording cannot be pseudonymised. Coordinator and provider both enforce this ([protocol](protocol-v0.1.md) § 4.2).
8. Local knowledge indexes (RAG) are not shared (open question, default: no).
9. **Minimisation and pseudonymisation (`reduced`):** own and trusted agents see the original. Before a subtask goes to a foreign agent, the coordinator shortens the conversation and replaces personal data with placeholders such as `[PERSON_1]`, `[ORG_1]`, `[LOCATION_1]` or `[EMAIL_1]`, tells the model to keep them, and swaps them back in the answer, also when streaming and in the arguments of tool calls.
   - **Minimisation:** if the request contains more than a single question (history, system instructions), an own or trusted model first rewrites the last request as a self-contained task with only what is needed to answer it (step with role `router`). Foreign agents, also as verifier or synthesiser, receive this task instead of the conversation. With tools, the history stays complete, because the model needs its calls and their results.
   - **Fixed rules** find email addresses, phone numbers, IBANs (with checksum), card numbers (Luhn check), IP addresses (except loopback) and URLs with credentials.
   - **Names, organisations and places** are marked by an own or trusted model (step with role `router`; it sees the original anyway). Only findings that occur verbatim in the original or in the shortened task count. In addition, the terms in `tasks.pseudonymize_terms` are always replaced.
   - **Without minimisation and recognition, nothing goes to strangers:** if no own or trusted model is available or one of the two steps fails, foreign agents get nothing for the whole request; with only foreign agents the request fails with 400.
   - **Providers** accept `reduced` from strangers only with `accept_public_tasks`, as for `public`.
   - **Limits:** shortening is done by a model and can drop something needed or keep something unnecessary; a model can miss names, and whatever identifies a person indirectly (a role, unusual details) stays in the text. Content that must not leave the trusted circle under any circumstances belongs in `trusted`.
10. **Images** in requests only go to agents that understand images (modality `image`, `vision: true` in the configuration), also as verifier or synthesiser. An image cannot be pseudonymised: with `reduced`, requests with images stay in the trusted circle. `public` is allowed as for text: whoever marks a request public decides that strangers may see its images; photos of people and scanned documents belong in `trusted`. Coordinator and provider both enforce this ([protocol](protocol-v0.1.md) § 4.2).

## Open

- How much may automatic pseudonymisation change the domain context? Placeholders keep the structure of a text but lose, for example, gender or region; whether realistic substitute values would serve better remains open. The same applies to how much shortening may lose.
- Confidential computing (TEE) remains a later option, not a promise.
