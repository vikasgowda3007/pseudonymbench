# Evaluation synthetic

Data: `data/synthetic-1-2000.jsonl` (2000 samples).

## Detector alone
**regex tier (detector only)**: 2000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 614 | 1.000 | 1.000 | 0.0% | 628 | 0.978 | 0.995 |
| EMAIL | 637 | 1.000 | 1.000 | 0.0% | 637 | 1.000 | 1.000 |
| IBAN | 606 | 1.000 | 1.000 | 0.0% | 606 | 1.000 | 1.000 |
| PHONE | 551 | 1.000 | 1.000 | 0.0% | 636 | 0.866 | 0.970 |
| SSN | 568 | 1.000 | 1.000 | 0.0% | 568 | 1.000 | 1.000 |

Overall precision (any changed region overlapping a gold span): 0.968 over 3075 replacements. Clean text (no PII): 34 of 500 samples had something masked (6.8%).

## Through the proxy
**pseudonymbench**: 2000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 614 | 1.000 | 1.000 | 0.0% | 628 | 0.978 | 0.995 |
| EMAIL | 637 | 1.000 | 1.000 | 0.0% | 637 | 1.000 | 1.000 |
| IBAN | 606 | 1.000 | 1.000 | 0.0% | 606 | 1.000 | 1.000 |
| PHONE | 551 | 1.000 | 1.000 | 0.0% | 636 | 0.866 | 0.970 |
| SSN | 568 | 1.000 | 1.000 | 0.0% | 568 | 1.000 | 1.000 |

Overall precision (any changed region overlapping a gold span): 0.968 over 3075 replacements. Clean text (no PII): 34 of 500 samples had something masked (6.8%).


## Request-field coverage

| request field | pseudonymbench |
|---|---|
| messages[].content (string) | masked |
| messages[].content[].text | masked |
| system message | masked |
| developer message | masked |
| assistant message | masked |
| assistant refusal | masked |
| tool_calls[].function.arguments (value) | masked |
| tool_calls[].function.arguments (key) | masked |
| tool message content | masked |
| tools[].function.description | masked |
| tools[].function.parameters (schema text) | masked |
| functions[].description (legacy) | masked |
| user | masked |
| metadata (value) | masked |
| metadata (key) | masked |
| stop | masked |
| prediction.content | masked |
| response_format.json_schema.description | masked |
| image_url.url (http, query string) | masked |
| unknown top-level field | masked |
| unknown top-level field (name) | masked |

Fields where the canary reached the upstream: pseudonymbench 0/21.
