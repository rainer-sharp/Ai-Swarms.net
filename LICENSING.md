# Licenses

**English** · [Deutsch](de/LICENSING.md)

AI-Swarms.net is a community project. There is no commercial license and none is planned.

## Code: GNU AGPL-3.0-or-later

The program code is free software under the [GNU Affero General Public License](LICENSE), version 3 or, at your option, any later version (`AGPL-3.0-or-later`). You may use, modify and distribute it without restriction, also in a company and also for money. The condition: whoever distributes modified versions **or makes them available to others over a network** must disclose their complete source code under the AGPL.

This explicitly allows, for example:

- running your own nodes, helpers and trackers, privately or in a company,
- distributing AI-Swarms.net unchanged or offering it as a service,
- changes that you publish under the AGPL.

## Documentation: CC BY 4.0

The documentation is licensed under [Creative Commons Attribution 4.0 International](LICENSE-DOCS) (CC BY 4.0). You may copy, modify and distribute it for any purpose, as long as you name the author, refer to the licence and mark changes. This makes it easier in particular to implement the protocol independently from the specification.

Documentation means:

- everything under `docs/`, including the overall concept (`docs/concept.md`, `docs/de/concept.md`, graphics) and its archived first version (`docs/archive/concept-2026-10-14-de.pdf`),
- everything under `spec/`,
- the guides `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `LICENSING.md`, `deploy/*/README.md`, `bench/README.md` and `bench/report-r1.md` and their German versions in the `de/` subfolders (e.g. `de/README.md`, `deploy/vps/de/README.md`),
- the graphics `deploy/raspberry-pi/*.svg`,
- the membership test vectors under `internal/membership/testdata/`, so that other implementations can use them freely.

Attribution for example: "AI-Swarms.net documentation, © 2026 Rainer Sestig and contributors, CC BY 4.0, github.com/rainer-sharp/Ai-Swarms.net".

Not included are the program code, configuration templates, scripts and the remaining test data (AGPL-3.0-or-later) as well as the legal texts `LICENSE`, `LICENSE-DOCS` and `DCO`.

## Components with their own licence

- The benchmark tasks in `bench/` come from HumanEval and GSM8K (© OpenAI, MIT licence, see `bench/LICENSE-human-eval` and `bench/LICENSE-gsm8k`).
- Included Go libraries are under their own licences (MIT, BSD, ISC, Apache-2.0, MPL-2.0).

## Contributions

Contributions are made under the licences above: code under AGPL-3.0-or-later, documentation under CC BY 4.0. Each commit carries a `Signed-off-by` line confirming the [Developer Certificate of Origin](DCO), see [CONTRIBUTING.md](CONTRIBUTING.md#license-and-sign-off). Contributors keep their copyright; there is no transfer of rights.
