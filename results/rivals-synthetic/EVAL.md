# Rivals: detection quality (data/synthetic-1-1000.jsonl)

## pseudonymbench-container
**pseudonymbench-container**: 1000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 314 | 1.000 | 1.000 | 0.0% | 320 | 0.981 | 0.996 |
| EMAIL | 346 | 1.000 | 1.000 | 0.0% | 346 | 1.000 | 1.000 |
| IBAN | 305 | 1.000 | 1.000 | 0.0% | 305 | 1.000 | 1.000 |
| PHONE | 268 | 1.000 | 1.000 | 0.0% | 310 | 0.865 | 0.970 |
| SSN | 267 | 1.000 | 1.000 | 0.0% | 267 | 1.000 | 1.000 |

Overall precision (any changed region overlapping a gold span): 0.969 over 1548 replacements. Clean text (no PII): 16 of 250 samples had something masked (6.4%).

## litellm-presidio
**litellm-presidio**: 1000 samples

| kind | gold | recall (overlap) | recall (exact span) | bytes left visible | predicted | precision (typed) | F2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 314 | 0.904 | 0.554 | 16.9% | 265 | 0.977 | 0.918 |
| EMAIL | 346 | 1.000 | 0.283 | 0.0% | 346 | 1.000 | 1.000 |
| IBAN | 305 | 1.000 | 0.787 | 2.3% | 305 | 1.000 | 1.000 |
| PHONE | 268 | 0.836 | 0.787 | 18.3% | 238 | 0.861 | 0.841 |
| SSN | 267 | 1.000 | 0.985 | 0.0% | 277 | 0.942 | 0.988 |

What the masker called each gold span (rows: gold kind):

| gold \ called | CARD | EMAIL | IBAN | PHONE | SSN | UNTYPED | MISSED |
|---|---:|---:|---:|---:|---:|---:|---:|
| CARD | 259 | 0 | 0 | 0 | 0 | 25 | 30 |
| EMAIL | 0 | 346 | 0 | 0 | 0 | 0 | 0 |
| IBAN | 0 | 0 | 305 | 0 | 0 | 0 | 0 |
| PHONE | 0 | 0 | 0 | 205 | 0 | 19 | 44 |
| SSN | 0 | 0 | 0 | 0 | 261 | 6 | 0 |

Overall precision (any changed region overlapping a gold span): 0.533 over 2676 replacements. Clean text (no PII): 224 of 250 samples had something masked (89.6%).

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
