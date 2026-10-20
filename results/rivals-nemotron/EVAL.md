# Rivals: detection quality (data/nemotron-pii.jsonl)

## pseudonymbench-container
**pseudonymbench-container**: 3000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 380 | 0.113 | 0.108 | 88.8% | 41 | 1.000 | 0.138 |
| EMAIL | 1221 | 0.998 | 0.991 | 0.1% | 1218 | 1.000 | 0.998 |
| IBAN | 0 | n/a | n/a | n/a | 1 | 0.000 | n/a |
| PHONE | 638 | 0.997 | 0.984 | 0.3% | 646 | 0.985 | 0.994 |
| SSN | 249 | 1.000 | 1.000 | 0.0% | 249 | 1.000 | 1.000 |

What the masker called each gold span (rows: gold kind):

| gold \ called | CARD | EMAIL | IBAN | PHONE | SSN | UNTYPED | MISSED |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 41 | 0 | 0 | 2 | 0 | 0 | 337 |
| EMAIL | 0 | 1218 | 0 | 0 | 0 | 0 | 3 |
| PHONE | 0 | 0 | 0 | 636 | 0 | 0 | 2 |
| SSN | 0 | 0 | 0 | 0 | 249 | 0 | 0 |

Overall precision (any changed region overlapping a gold span): 0.995 over 2156 replacements. Clean text (no PII): 0 of 0 samples had something masked (0.0%). 646 further replacements touched only out-of-scope PII (names, addresses, ...) and are not counted.

## litellm-presidio
**litellm-presidio**: 3000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 380 | 0.687 | 0.274 | 50.7% | 41 | 1.000 | 0.733 |
| EMAIL | 1221 | 0.998 | 0.046 | 0.1% | 1213 | 0.999 | 0.998 |
| IBAN | 0 | n/a | n/a | n/a | 1 | 0.000 | n/a |
| PHONE | 638 | 0.998 | 0.922 | 0.2% | 663 | 0.881 | 0.972 |
| SSN | 249 | 1.000 | 0.968 | 0.0% | 246 | 1.000 | 1.000 |

What the masker called each gold span (rows: gold kind):

| gold \ called | CARD | EMAIL | IBAN | PHONE | SSN | UNTYPED | MISSED |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 41 | 0 | 0 | 52 | 0 | 168 | 119 |
| EMAIL | 0 | 1212 | 0 | 2 | 0 | 4 | 3 |
| PHONE | 0 | 7 | 0 | 584 | 0 | 46 | 1 |
| SSN | 0 | 0 | 0 | 1 | 246 | 2 | 0 |

Overall precision (any changed region overlapping a gold span): 0.522 over 4527 replacements. Clean text (no PII): 0 of 0 samples had something masked (0.0%). 13510 further replacements touched only out-of-scope PII (names, addresses, ...) and are not counted.

# Rivals: request-field coverage

| request field | pseudonymbench-container | litellm-presidio |
|---|---|---|
| messages[].content (string) | masked | masked |
| messages[].content[].text | masked | masked |
| system message | masked | masked |
| developer message | masked | masked |
| assistant message | masked | masked |
| assistant refusal | masked | LEAKED |
| tool_calls[].function.arguments (value) | masked | LEAKED |
| tool_calls[].function.arguments (key) | masked | LEAKED |
| tool message content | masked | masked |
| tools[].function.description | masked | LEAKED |
| tools[].function.parameters (schema text) | masked | LEAKED |
| functions[].description (legacy) | masked | LEAKED |
| user | masked | dropped |
| metadata (value) | masked | dropped |
| metadata (key) | masked | dropped |
| stop | masked | LEAKED |
| prediction.content | masked | LEAKED |
| response_format.json_schema.description | masked | LEAKED |
| image_url.url (http, query string) | masked | LEAKED |
| unknown top-level field | masked | LEAKED |
| unknown top-level field (name) | masked | LEAKED |

Fields where the canary reached the upstream: litellm-presidio 12/21, pseudonymbench-container 0/21.
Fields the proxy removed instead of masking (counted as not leaked): litellm-presidio 3.
