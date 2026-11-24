# SVG fit

How the viewer sizes SVG. Resize the window, or the contents panel, to see each one follow the room it has.

An SVG is drawn at its natural width, or wider until its small print reaches 12px or its typical text reaches 16px, whichever comes first. It is never wider than the panel less its gutters. One wider than this column of text is centred on the column.

## A wide drawing

Natural width 1400px, labels 14px. It leaves the column and takes the room there is, up to its natural size.

![Request path](wide-architecture.svg)

## A drawing with small print

Natural width 360px, labels 8px. It grows by half, to 540px, where its labels reach 12px.

![Build steps](small-print.svg)

## A drawing already at reading size

Natural width 360px, labels 16px. It stays as it is.

![Client and server](reading-size.svg)

## A drawing with no text

Nothing to judge a size by, so it stays natural.

![Shapes](no-text.svg)

## A wide diagram

```mermaid
flowchart LR
  A[Markdown source] --> B[Math and diagram preprocessors] --> C[goldmark] --> D[HTML template] --> E{Output}
  E -->|file| F[WeasyPrint or Prince] --> G[PDF]
  E -->|view| H[Viewer page] --> I[Browser]
  E -->|clipboard| J[Rich text] --> K[Paste target]
```

## A small diagram

```mermaid
flowchart TD
  A[Open] --> B[Read]
```

## A diagram with small text

```mermaid
gantt
  title Release plan
  dateFormat YYYY-MM-DD
  section Build
  Design :a1, 2026-01-05, 10d
  Implement :a2, after a1, 20d
  section Ship
  Verify :b1, after a2, 7d
  Release :after b1, 2d
```

## A diagram its author sized

A `vellum:scale 0.5` hint keeps it to half the column.

<!-- vellum:scale 0.5 -->
```mermaid
flowchart LR
  A[Request] --> B[Queue] --> C[Worker] --> D[Store]
```

## Left alone

An image inside a sentence, like this one ![Build steps](small-print.svg), keeps its natural size.
