# Evaluation nemotron

Data: `data/nemotron-pii.jsonl` (3000 samples).

## Detector alone
**regex tier (detector only)**: 3000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 380 | 0.113 | 0.108 | 88.8% | 41 | 1.000 | 0.138 |
| EMAIL | 1221 | 0.998 | 0.998 | 0.1% | 1218 | 1.000 | 0.998 |
| IBAN | 0 | n/a | n/a | n/a | 1 | 0.000 | n/a |
| PHONE | 638 | 0.997 | 0.997 | 0.3% | 646 | 0.985 | 0.994 |
| SSN | 249 | 1.000 | 1.000 | 0.0% | 249 | 1.000 | 1.000 |

Overall precision (any changed region overlapping a gold span): 0.996 over 2155 replacements. Clean text (no PII): 0 of 0 samples had something masked (0.0%). 646 further replacements touched only out-of-scope PII (names, addresses, ...) and are not counted.

## Through the proxy
**pseudonymbench**: 3000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 380 | 0.113 | 0.108 | 88.8% | 41 | 1.000 | 0.138 |
| EMAIL | 1221 | 0.998 | 0.991 | 0.1% | 1218 | 1.000 | 0.998 |
| IBAN | 0 | n/a | n/a | n/a | 1 | 0.000 | n/a |
| PHONE | 638 | 0.997 | 0.984 | 0.3% | 646 | 0.985 | 0.994 |
| SSN | 249 | 1.000 | 1.000 | 0.0% | 249 | 1.000 | 1.000 |

Overall precision (any changed region overlapping a gold span): 0.995 over 2156 replacements. Clean text (no PII): 0 of 0 samples had something masked (0.0%). 646 further replacements touched only out-of-scope PII (names, addresses, ...) and are not counted.


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
