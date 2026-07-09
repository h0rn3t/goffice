# Paragraph/run style inheritance, theme colors, and numbering

Date: 2026-07-09  
Status: approved  
Scope: README gap A — styles.xml paragraph/run inheritance, theme colors, numbering (N2)

## Goal

Resolve paragraph and run formatting through `styles.xml` (including `basedOn` and `docDefaults`), map theme text colors via `theme1.xml`, and render Word lists from `numbering.xml` as marker text + hanging indent — all at `Open` time, so layout/render keep consuming an already-resolved `Document`.

Out of this change (still README gaps): `tblStylePr`, images, headers/footers, fields/hyperlinks, footnotes, multi-section/`w:cols`, fonts beyond Liberation mapping, `w:tcMar`, repeating header rows, vertical/diagonal cell text.

## Approach

Extend the existing in-package resolver pattern (same as spacing and table styles today). No new packages. Optional package parts degrade to empty context; only `word/document.xml` remains required.

## Data flow

```
Open(docx)
  ├─ word/document.xml      → body
  ├─ word/styles.xml        → para/run styles + docDefaults (optional)
  ├─ word/theme/theme1.xml  → theme color map (optional)
  └─ word/numbering.xml     → abstractNum / num (optional)
        ↓
styleContext + numberingContext
        ↓
buildBody (document order)
  ├─ paragraph: resolve pPr + run props + optional list marker
  └─ table cells: same paragraph builder
        ↓
Document{Body, Geometry}
```

## Resolution priority

For each property field independently (nearest declared value wins):

1. Inline `w:pPr` / `w:rPr` on the paragraph or run
2. `w:rStyle` on the run (character style + `basedOn`), when resolving run props
3. `w:pStyle` on the paragraph (paragraph style + `basedOn`), including that style’s `w:rPr` for run props and `w:numPr` for lists
4. `w:docDefaults` (`pPrDefault` / `rPrDefault`)
5. Hardcoded defaults (`Calibri`, 11pt, left align, no indent, black text)

Unknown style IDs, num IDs, levels, or theme slots skip that tier and continue down the chain.

## Paragraph styles (A1)

Extend `resolvedParaStyle` beyond spacing:

- `jc` → `Alignment`
- `ind` → `Indent` (left/right/firstLine/hanging; `w:start`/`w:end` still out of scope)
- `spacing` (already implemented)
- `numPr` reference (`numId` + `ilvl`) for list resolution

Merge along `basedOn` per field, same shape as `mergeSpacing` / `mergeBorders`.

`buildParagraph` resolves alignment and indent with the same inline → style → docDefaults → zero/left path already used for spacing.

## Run styles (A2)

New `resolvedRunStyle` (and character-style map keyed by `w:styleId` where `w:type="character"`):

- bold / italic / underline
- `rFonts` (ascii / hAnsi only; complex-script fonts out of scope)
- `sz` (half-points → points)
- `color` (raw OOXML value + optional theme hook; see A3)

Sources when building a run:

inline `w:rPr` → `w:rStyle` (+ `basedOn`) → paragraph style’s `w:rPr` (+ `basedOn`) → `rPrDefault` → defaults.

Toggle properties (`w:b`, `w:i`): presence means on unless `w:val` is `false`/`0`/`off`. A declared value (including explicit off) wins over the parent during merge.

Add `w:rStyle` to `xmlRPr` and resolve it in `buildParagraph`.

Out of scope: linked styles (`w:link`), `w:bCs`/`w:iCs`, `w:vanish`, `w:strike`, highlight as run background.

## Theme colors (A3)

Public model:

- `RunProperties.Color` is `"#RRGGBB"` or `""` (empty → backend default black).

Renderer:

- Before `DrawText`, set text color from the run’s resolved color; empty → black.
- Tests’ fake renderer records color with each draw; `fpdfRenderer` uses `SetTextColor`.

Theme load:

- Optional `word/theme/theme1.xml` in `Open`.
- Map slots `dk1`, `lt1`, `dk2`, `lt2`, `accent1`…`accent6`, `hlink`, `folHlink` to sRGB hex from the theme’s color scheme.
- `w:color`: prefer theme slot when `w:themeColor` (or equivalent theme-valued val) is present; else parse `w:val` hex; `auto` / missing → `""`.
- Unknown theme slot → treat as unresolved (`""` / black path).

Out of scope: theme fonts (major/minor), shade/tint on theme colors, paragraph/run `w:shd`, `w:highlight`.

## Numbering N2 (A4)

### Parse `word/numbering.xml`

- `w:abstractNum` → levels (`ilvl`, `numFmt`, `lvlText`, `start`, `lvlJc`, optional `pPr/ind`, optional marker `rPr`)
- `w:num` → `numId` → `abstractNumId`, plus `lvlOverride` (start / fmt) when present

### Resolve `numPr`

Inline `w:pPr/w:numPr` → paragraph style’s `numPr` (+ `basedOn`) → no list.

### Counters

During `buildBody` (document order, including paragraphs inside tables):

- State: per `numId`, per `ilvl` counter
- On a list paragraph at level L: if L was never used for this `numId`, set counter[L] = `start` (from level or `lvlOverride`, default 1); else increment counter[L] by 1. Reset all levels > L; leave levels < L unchanged. The marker uses the value after this update.

### Marker → model

- Substitute `%1`…`%9` in `lvlText` using formats: `decimal`, `lowerLetter`, `upperLetter`, `lowerRoman`, `upperRoman`, `bullet`
- Bullet with empty/missing `lvlText` → `•`
- Unknown `numFmt` → behave as `decimal`
- Prepend a synthetic first run: marker text + a single trailing space (no tab-stop engine; `w:suff` beyond that stays out of scope). Run props: resolve the level’s `rPr` if present, else the same docDefaults → hardcoded path used for a run with no inline `rPr` (do not copy the first content run).
- Indent: if the paragraph did not declare its own `ind`, apply the level’s `pPr/ind` (typically hanging so the marker sits left of the text body)

Out of scope: picture bullets, `isLgl`, suffix modes beyond tab/space, restarts outside `lvlOverride`, numbering in headers/footers.

## Error handling

| Part | Missing / unreadable / malformed |
|------|----------------------------------|
| `styles.xml` | empty style maps, no docDefaults (unchanged policy) |
| `theme1.xml` | empty theme map |
| `numbering.xml` | no lists (paragraphs render without markers) |

`Open` fails only when `word/document.xml` is missing or invalid. Conversion still skips unsupported content and returns a successful PDF.

## Testing

Extend `internal/docxtest` with helpers to inject `word/theme/theme1.xml` and `word/numbering.xml` alongside existing `Build` / `BuildWithStyles`.

Coverage:

1. **Paragraph:** pStyle jc/ind via `basedOn`; inline overrides style; docDefaults under style
2. **Run:** rPr from pStyle / rStyle / rPrDefault; explicit toggle off in style; inline wins
3. **Theme:** themeColor → hex; plain hex `w:val`; absent theme1
4. **Numbering:** decimal 1/2/3; nested level reset; bullet; lowerLetter; `lvlText` with `%1.`; hanging indent; numPr from style; `lvlOverride` start
5. **Smoke render:** colored run + list paragraph through fake/fpdf path

## README

Move A1–A4 out of “поки не обробляється” into “підтримується”. Leave remaining gaps (tblStylePr, images, headers, fields/hyperlinks, footnotes, multi-section/cols, non-Liberation fonts, tcMar/header rows, vertical/diagonal cell borders) listed as unsupported.

## Non-goals (this change)

- New public API surface beyond `RunProperties.Color`
- Separate `document/styles` or `document/numbering` packages
- Resolving styles at layout time instead of `Open`
- Pixel-perfect Word list/theme parity (legal numbering, picture bullets, theme tint/shade)
