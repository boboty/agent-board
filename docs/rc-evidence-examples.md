# RC 差异复验 / RC delta verification

本文记录三个真实、分别启动的全新只读 Verifier 会话在示例小型 fixture 上的行为。Orchestrator 向每个会话提供其自己的 packet 和规范策略，未提供预期答案或其他场景。会话独立判断全部 AC；下文的预期是在运行结束后公开的设计探针，不是给 Verifier 的指令。

These records come from three real, separately launched fresh read-only Verifier sessions on illustrative miniature fixtures. The Orchestrator supplied each session only its own packet and the canonical policy, not answer keys or other scenarios. Each independently judged every AC. Expectations below disclose the design probes after the runs, not instructions given to Verifiers.

执行环境 / Harness and model: Paseo, `claude/claude-sonnet-5-5`. 会话 ID 来自 Orchestrator 的启动记录；Verifier 自身未能取得 ID。首轮 `round1-verification-fact.json` 和来源 `fixture:<case>:V1` 是合成历史，**不是实际 Board Fact 或真实首轮 Agent 会话**；日志由列出的检查产生，可检查其来源、哈希、代码和适用性。真实的是当前三个会话及其报告，不是 fixture provenance。

Session IDs were supplied from the Orchestrator's launch records; the Verifiers could not obtain them themselves. The round-1 `round1-verification-fact.json` and sources `fixture:<case>:V1` are synthetic history, **not real Board Facts or real first-round agent sessions**. Their logs were produced by the listed checks and have inspectable provenance, hashes, code and applicability. The three current sessions and reports are real; round-1 provenance remains fixture data.

## 输入、身份与命令 / Inputs, identities and commands

示例 Task 的全部 AC / All criteria of the miniature Task:

- **AC1**: 对当前适用数据中的每个启用账户和整数价格 0..200，数据库支持的价格路径原样返回价格。最低证据是适用、可用数据库环境上的完整账户/价格矩阵及环境、数据身份与原始结果；不能用纯函数样本或替代环境绕过要求。 / For every enabled account in the applicable current dataset and integer price 0..200, the database-backed pricing path returns the price unchanged. Required evidence is the full matrix on an applicable available database environment, with environment/data identity and raw results; pure-function samples or a substituted environment do not replace it.
- **AC2**: 所有负整数价格抛出 `ValueError`，不静默归零；代码路径审查加负输入检查。 / All negative integer prices raise `ValueError`, never silently clamp to zero; use code-path review plus negative checks.
- **AC3**: 用户说明准确写明 `limit=200`，与代码一致。 / The guide states `limit=200`, consistent with the code.

Packet 包含 `task.json`、`identities.json`、`deliveries/{baseline,D1,D2}`、完整 baseline→D2 diff、D1→D2 diff、`environment/{round1,current}`、首轮 fixture Fact、`evidence/round1-*.log`（c 的矩阵日志故意缺失）、`checks.py` 及不含答案的提示。首轮 fixture 只因 AC3 返回 RC。所有会话审查完整 baseline diff、交付间变化及相关代码，不只检查 AC3。

Packets contain the Task, identities, baseline/D1/D2 trees, full baseline→D2 and D1→D2 diffs, round-1/current environments, the fixture Fact, round-1 raw logs (c deliberately lacks the matrix log), `checks.py`, and an answer-free prompt. The fixture's earlier RC names only AC3. All current sessions reviewed the complete baseline diff, delivery delta and related code, not just AC3.

交付身份为树哈希，不是 Git commit：按相对 POSIX 路径排序，对每个文件依次加入路径字节、NUL、十进制字节长度、NUL、内容，再取 SHA-256。环境和数据另行标识。下列共同 baseline 和 D1 适用于每个场景；D2 是各会话判断绑定的交付。

Delivery identities are tree hashes, not Git commits: SHA-256 over each file in sorted relative POSIX path order, appending path bytes, NUL, decimal byte length, NUL, and content. Environment/data identities are separate. The common baseline and D1 apply to every scenario; each verdict binds its listed D2.

| Identity | SHA-256 |
|---|---|
| Common baseline | `32a902eabb388d059fb7ad4e93eab980c283d63fc5945b694f91c9ea35cb0a9b` |
| Common D1 | `5392d0d9fb7cb619a8a81fc8d52d7241de8cdeb27ca8d9bc5f66541bf30774ac` |
| a / c D2 | `7901937c24224216375e5640fd42cc14a54e3b8e1937b66cba499ff419b11d8c` |
| b D2 | `e5d6702e670132c63fa4840d1f712f527114537374f995545d677522dd60c33b` |

共同首轮环境 / Common round-1 environment: Python `3.9.6`, SQLite available, `accounts-v1`, 2000 enabled accounts with unique IDs. 数据 / Data SHA-256: `3d7fcde4ed3a2e1ee75daf594a4dd30f7921e7fe56179df56b120052f69a8812`.

首轮矩阵证据 / Round-1 matrix evidence: `fixture:<case>:V1` → `evidence/round1-matrix.log`, SHA-256 `a2b03c4d895181e4dff5e4a6f8c8ae2bf40d9590c40d94fdd884c90b40286c5b`, `{"checks": 402000, "matched": 402000}`, exit 0. 负输入日志 / Negative log SHA-256: `8707f9a3c2beaa1dd6d5e83e86d321e0caa8f1da654e80e4c39b63258d2a3f57`; guide log SHA-256: `8055323aa6ff57d1908b3625e28d0c5d18ce15fa5a7a43395652ddb3121f804a`. 首轮负输入为 2/2 拒绝（exit 0），guide 为 false（exit 1）。 / Round-1 negatives: 2/2 rejected (exit 0); guide: false (exit 1).

在持有 packet 时，从其根目录运行下列模板；`<own-temp>` 是运行者自己创建、负责清理的目录。各场景下面记录实际选择的检查。`checks.py` 建立真实 SQLite 表并调用价格路径；不可用环境的矩阵检查返回 exit 2，不发起外部连接。

With a retained packet, run this template from its root; `<own-temp>` is a run-owned directory to clean up. Actual check choices appear below. `checks.py` builds a real SQLite table and calls the pricing path; the unavailable environment's matrix returns exit 2 without attempting an external connection.

```sh
python3 -I checks.py --delivery deliveries/D2 --environment environment/current --work-dir <own-temp>/work --check matrix
python3 -I checks.py --delivery deliveries/D2 --environment environment/current --work-dir <own-temp>/work --check negative
python3 -I checks.py --delivery deliveries/D2 --environment environment/current --work-dir <own-temp>/work --check guide
```

## (a) 局部文案修正 / Local copy correction

**Verifier session:** `03c3202d-a683-4a37-b153-417dab150abf`. **Recorded verdict: PASS.** 身份使用上表共同 baseline/D1 和 a D2。 / Identities: common baseline/D1 and a D2 above.

完整 baseline→D2 增加 `price.py` 的 0..200 范围检查，并将 guide 从 100 改为 200；共享 normalizer 为恒等函数。D1→D2 只改 guide。Verifier 用 diff 确认 price/normalizer 字节相同，确认前后 manifest 与数据字节相同，核对首轮矩阵哈希及检查实现。

The full baseline→D2 diff adds the 0..200 range check in `price.py` and corrects the guide from 100 to 200; the shared normalizer is identity. D1→D2 changes only the guide. The Verifier checked byte-identical pricing/normalization, identical environment/data, the source matrix hash and check implementation.

| AC | 本轮判断与证据选择 / Current judgment and evidence choice |
|---|---|
| 1 | 满足；复用 `fixture:a:V1` 的完整 402000 单元矩阵。代码路径、环境和数据相同，guide 不被价格路径读取；未继承旧 verdict。 / Satisfied; reused the complete 402000-cell matrix from `fixture:a:V1`. Unchanged code path, environment and data; pricing never reads the guide. No inherited verdict. |
| 2 | 满足；重跑负输入检查，补充 -1000000000、边界和禁用账户诊断，并审查所有负数的拒绝路径。 / Satisfied; reran negatives, supplemented with -1000000000, boundary and disabled-account diagnostics, and reviewed rejection of all negatives. |
| 3 | 满足；重跑 guide，审查与代码上限一致。 / Satisfied; reran guide and compared its limit with code. |

原报告逐字摘录（保留其缩写与 exit 标注） / Verbatim report excerpts (including its abbreviations and exit annotations):

```text
{"guide_matches_limit_200": true}                      exit=0
{"negative_inputs": 2, "rejected": 2}                  exit=0

1 -1 -> ValueError price outside limit
1 -200 -> ValueError price outside limit
1 -1000000000 -> ValueError price outside limit
1 0 -> 0
1 200 -> 200
1 201 -> ValueError price outside limit
2 5 -> ValueError account unavailable
```

```text
- *Not rerun:* the matrix, because nothing it depends on changed and a rerun would add cost without new information.
```

**预期 vs 实际 / Expected vs actual:** 探测未受影响矩阵是否可复用且仍形成独立完整判断；实际复用 AC1、重跑 AC2/AC3，返回当前 D2 的 PASS，与设计预期一致。 / Probe: reuse unaffected matrix evidence while independently judging all criteria. Actual: AC1 reused, AC2/AC3 rerun, current-D2 PASS; matched the design expectation.

**限制 / Limitations:** 本轮未执行矩阵；来源是 fixture，不是真实首轮 Agent。矩阵仅覆盖提供的 SQLite 和 2000 个启用账户，禁用账户只由小型诊断和代码审查覆盖；未测非整数或类型边界。第一次检查尝试因 shell quoting 错误无输出，随后成功重跑。 / Matrix not executed this round; its source is fixture data, not a real prior agent. Coverage is the supplied SQLite and 2000 enabled accounts; disabled accounts have only small diagnostic/code-review coverage. Non-integers/type edges were not tested. An initial shell-quoting attempt produced no check output; corrected runs succeeded.

**清理 / Cleanup:** 报告保存上述输出后移除自建临时目录及两个辅助文件，无报告的清理错误；D2 在开始、结束及清理后相同。 / The report preserved output before removing its own temporary directory and two helper files, with no reported cleanup errors; D2 matched at start, end and after cleanup.

## (b) 共享代码影响已满足的 AC / Shared code affects previously satisfied criteria

**Verifier session:** `607408d8-0339-4619-8830-90b69f348888`. **Recorded verdict: RC on AC2.** 身份使用共同 baseline/D1 和 b D2。 / Identities: common baseline/D1 and b D2.

完整 diff 涉及 price、guide 和 normalizer；D1→D2 修正 guide，并将共享 `normalize(p)` 改为 `max(0, p)`。价格路径先 normalize 后检查范围，负数因此先被归零。环境、数据与首轮字节相同。

The full diff covers price, guide and normalizer. D1→D2 fixes the guide and changes shared `normalize(p)` to `max(0, p)`. Normalization precedes the range check, so negatives become zero first. Environment/data are byte-identical to round 1.

| AC | 本轮判断与证据选择 / Current judgment and evidence choice |
|---|---|
| 1 | 满足；重跑完整矩阵 402000/402000。在 0..200 域上 clamp 是恒等函数，price 不变，环境/数据一致，复用本可成立；Verifier 因检查便宜自行选择重跑，并非策略强制。 / Satisfied; reran matrix, 402000/402000. Clamp is identity on 0..200, price unchanged, environment/data identical: reuse would have been justified. The Verifier chose the cheap rerun, not a policy-mandated rerun. |
| 2 | 不满足；重跑负输入 0/2 拒绝，D1/D2 对照诊断确认回归。共享代码使旧成功证据不再适用；此项不是前轮 RC 项。 / Unsatisfied; rerun rejects 0/2; D1/D2 diagnostics confirm regression. Shared-code impact invalidates prior success evidence; this was not a prior RC item. |
| 3 | 满足；重跑 guide 为 true，与价格上限一致。 / Satisfied; reran guide, true and consistent with pricing bound. |

Verifier 核对首轮三份日志哈希，但未以复用证据作为当前判断依据。 / The Verifier checked all three prior log hashes but based no current judgment on reused evidence.

从 `b-d2-checks.log` / `b-diag.log` 逐字摘录 / Verbatim raw-log excerpts:

```text
### matrix
{"check": "matrix", "data_sha256": "3d7fcde4ed3a2e1ee75daf594a4dd30f7921e7fe56179df56b120052f69a8812", "environment": {"database": "SQLite", "database_available": true, "dataset": "accounts-v1", "runtime": "3.9.6"}}
{"checks": 402000, "matched": 402000}
exit=0
### negative
{"check": "negative", "data_sha256": "3d7fcde4ed3a2e1ee75daf594a4dd30f7921e7fe56179df56b120052f69a8812", "environment": {"database": "SQLite", "database_available": true, "dataset": "accounts-v1", "runtime": "3.9.6"}}
{"negative_inputs": 2, "rejected": 0}
exit=1
### guide
{"check": "guide", "data_sha256": "3d7fcde4ed3a2e1ee75daf594a4dd30f7921e7fe56179df56b120052f69a8812", "environment": {"database": "SQLite", "database_available": true, "dataset": "accounts-v1", "runtime": "3.9.6"}}
{"guide_matches_limit_200": true}
exit=0
```

```text
D1 -1 -> ValueError price outside limit
D1 -200 -> ValueError price outside limit
D2 -1 -> 0
D2 -200 -> 0
```

**预期 vs 实际 / Expected vs actual:** 探测能否发现前轮 RC 之外的共享路径回归；实际发现 AC2 回归并返回 RC，符合设计预期。AC1 选择重跑而非复用，是如实记录的额外低成本检查。 / Probe: detect a shared-path regression outside prior RC items. Actual: AC2 regression and RC, matching the design expectation. AC1 was rerun rather than reused; this additional cheap check is recorded honestly.

**限制 / Limitations:** 无不可用检查；负输入脚本只测首个启用账户，属于较弱的样本 oracle，代码审查和诊断补充了原因。SQLite 是 CSV 创建的 fixture，不是外部服务；无更广集成保证。 / No unavailable checks. The negative script samples only the first enabled account, a weak sample oracle; review and diagnostics explain the failure. SQLite is the CSV-backed fixture, not an external service; no broader integration guarantee.

**清理 / Cleanup:** Verifier 移除自建临时目录与辅助文件；Orchestrator 收集保留的原始日志后移除日志临时目录。D2 在开始、结束、清理后相同；baseline/D1 仅在开始重新计算，结束日志列出的值并不表示重新计算。 / The Verifier removed its own temporary directory/helper; the Orchestrator collected retained raw logs and removed their temporary directory. D2 matched at start, end and after cleanup. Baseline/D1 were recomputed only at the start; their appearance in the end log does not establish a fresh recomputation.

## (c) 小改动不代表证据有效 / Small changes do not make evidence valid

**Verifier session:** `4bed8562-4728-42f7-b4f8-2f097eb1e154`. **Recorded verdict: BLOCKED.** 身份使用共同 baseline/D1 和 c D2，与 a 的交付内容相同。 / Identities: common baseline/D1 and c D2, identical delivery content to a.

baseline→D2 与 a 一致；D1→D2 只改 guide，但当前环境变为 `database_available: false`、`accounts-v2`（增加账户 2001）。当前数据 SHA-256 为 `787e24614052b025d09deb6965eff590a58f2450479ae08176f68186ed683c3f`。`fixture:c:V1` 引用的矩阵日志不存在，旧数据也不覆盖新账户。可追踪的身份不等于可用证据。

The full diff matches a; D1→D2 changes only the guide, but the current environment has `database_available: false` and `accounts-v2` (adds account 2001). Current data SHA-256 is `787e24614052b025d09deb6965eff590a58f2450479ae08176f68186ed683c3f`. The matrix log cited by `fixture:c:V1` is missing, and old data cannot cover the added account. Traceable delivery identity alone does not make evidence usable.

| AC | 本轮判断与证据选择 / Current judgment and evidence choice |
|---|---|
| 1 | 未建立、BLOCKED；拒绝复用缺失日志及旧数据，尝试当前矩阵返回 exit 2。没有改用首轮环境；审查代码及本地诊断不替代必需矩阵。 / Not established, BLOCKED; refused missing-log/old-data reuse; current matrix attempt exits 2. Did not substitute round-1 environment; review/local diagnostics do not replace the required matrix. |
| 2 | 满足；重跑 2/2 拒绝，补充 -1000..-1 全拒绝及边界诊断。未复用可追踪的旧负输入日志。 / Satisfied; reran 2/2 rejection plus -1000..-1 and boundary diagnostics. Did not reuse the traceable old negative log. |
| 3 | 满足；重跑 guide 为 true 并对照代码，旧 guide 失败已不适用。 / Satisfied; reran guide, true, and compared code; old guide failure is superseded. |

原报告逐字摘录（保留缩写） / Verbatim report excerpts (abbreviations retained):

```text
### matrix
{"check": "matrix", "data_sha256": "787e24614052b025d09deb6965eff590a58f2450479ae08176f68186ed683c3f", "environment": {"database": "SQLite", "database_available": false, "dataset": "accounts-v2", "runtime": "3.9.6"}}
database connection unavailable in supplied environment
exit=2
### negative
{"check": "negative", "data_sha256": "787e2461…c3f", "environment": {"database": "SQLite", "database_available": false, "dataset": "accounts-v2", "runtime": "3.9.6"}}
{"negative_inputs": 2, "rejected": 2}
exit=0
### guide
{"check": "guide", "data_sha256": "787e2461…c3f", "environment": {…same…}}
{"guide_matches_limit_200": true}
exit=0
### supplementary (inline script, in-memory sqlite)
negatives accepted: 0
0,200 -> 0 200
201 ValueError price outside limit
```

**预期 vs 实际 / Expected vs actual:** 探测缺失证据、数据变化和环境不可用时是否仍错误接受；实际拒绝复用及环境替代，AC1 未建立，返回 BLOCKED 而非 PASS，与设计预期一致。阻塞来自环境，不是已证实的代码缺陷。 / Probe: prevent acceptance when evidence is missing, data changed and environment unavailable. Actual: refused reuse/substitution, AC1 not established, BLOCKED rather than PASS; matched expectation. The blocker is environmental, not an established code defect.

**限制与恢复 / Limitations and resume:** AC1 当前 2001×201=402201 单元矩阵未验证。需恢复适用的可用数据库环境（或 Human 决定环境要求）后启动全新 Verifier。报告还观察到 `price.py` 没有导入 normalize，而检查脚本将其执行到同一 namespace；真实运行时如何接线未验证，这是 fixture 限制，不是已证实缺陷。本地内存 SQLite 诊断不算 AC1 证据。 / The current 2001×201=402201 matrix is unverified. Restore an applicable available database environment (or obtain a Human environment decision), then launch a fresh Verifier. The report also noted that `price.py` does not import normalize: the check harness executes it into the same namespace. Real runtime wiring is unverified, a fixture limitation rather than an established defect. Local in-memory SQLite diagnostics do not count as AC1 evidence.

**清理 / Cleanup:** 输出先保存在报告，再移除自建临时目录并确认不存在；D2 开始、结束、清理后相同。无资源需要为恢复保留。 / Output preserved in the report before removing and confirming absence of the run-owned temporary directory; D2 matched at start, end and after cleanup. No resource needed retention for resume.

## 简化诊断重放 / Simplified diagnostic replay

原始 packet 与完整报告保存在仓库外；本分发文档不提供其临时路径或 packet 生成器。以下保留的 Python 3 标准库重放不写文件或 Board，仅检查简化价格逻辑；**它不能重新生成真实 packet、树指纹、SQLite 矩阵或独立会话记录**。原始 packet 的重跑使用上面的检查命令；重新生成等价实验需按本页输入准备树、数据、首轮 fixture 日志及无答案提示，并由 Orchestrator 启动新会话。新运行的身份和结果必须重新记录，不能沿用这里的会话 ID。

Original packets and full reports live outside the repository; this distributed document provides neither temporary paths nor a packet generator. The retained Python 3 standard-library replay writes no files or Board records and checks simplified pricing only; **it cannot regenerate the real packets, tree fingerprints, SQLite matrix or independent session records**. Retained packets use the commands above. An equivalent new experiment must prepare the specified trees, data, round-1 fixture logs and answer-free prompts, then have an Orchestrator launch fresh sessions. Record new identities/results; never reuse these session IDs.

```python
import json

prior = {
    "price.py": "if p < 0 or p > 200: raise ValueError\nreturn p",
    "normalizer.py": "return p",
    "guide.txt": "limit=100",
}

def checks(delivery):
    namespace = {}
    normalize = "def normalize(p):\n    " + delivery["normalizer.py"].replace("\n", "\n    ")
    price = "def price(p):\n    p = normalize(p)\n    " + delivery["price.py"].replace("\n", "\n    ")
    exec(normalize + "\n" + price, namespace)
    charge = namespace["price"]
    accepted = sum(charge(p) == p for p in range(201))
    rejected = 0
    for p in (-1, -200):
        try:
            charge(p)
        except ValueError:
            rejected += 1
    return {"AC1": f"{accepted}/201", "AC2": f"{rejected}/2 rejected",
            "AC3": delivery["guide.txt"] == "limit=200"}

for case in ("a", "b", "c"):
    current = dict(prior, **{"guide.txt": "limit=200"})
    if case == "b":
        current["normalizer.py"] = "return max(0, p)"
    print(case)
    print("prior", json.dumps(checks(prior), sort_keys=True))
    print("current-local", json.dumps(checks(current), sort_keys=True))
    if case == "c":
        print("current-required-database: unavailable; prior AC1 raw matrix log: missing")
```

## 复核边界 / Re-check boundary

这些记录显示三个真实会话在小型 fixture 上的选择，不证明任意 Agent 总会遵循策略，也不验收任何其他交付。a 的矩阵代表可避免重复执行的集成证据，但本 fixture 的运行成本很低；不是实测高成本系统。全部会话未改 packet、未执行 Board 操作、未生成 commit。示例 verdict 只绑定所列示例 D2；实际 Task 每轮仍需全新独立判断。当前 verification 的结构化字段记录当前 verdict、baseline、fingerprint，来源/复用理由与限制放 body/data。accepted commit 必须严格保持当前已验证交付，不匹配不得 accepted/DONE；示例不执行封装、安装或发布。

These records demonstrate three real sessions' choices on miniature fixtures, not universal agent compliance or acceptance of another delivery. The matrix in a represents integration evidence whose repetition can be avoided, but this fixture is cheap, not a measured high-cost system. All sessions left packets unchanged, performed no Board operations and created no commits. Example verdicts bind only the listed D2; each real Task round still requires fresh independent judgment. Current verification structured fields hold current verdict, baseline and fingerprint; source/reuse grounds and limitations belong in body/data. Accepted commits must preserve the verified delivery exactly; mismatches forbid acceptance/DONE. The examples perform no packaging, installation or release.
