# Plati Design System — Agent Guide

Apply these rules when creating or updating frontend pages and components.
The design language is extracted from the landing page (`landing/index.html`).

---

## Design Tokens

### Colors

| Token | Tailwind class | Hex | Usage |
|-------|---------------|-----|-------|
| Primary | `primary` | `#2D7A5F` | Buttons, links, active tabs, brand |
| Primary dark | `primary-dark` | `#235f4a` | Hover states |
| Primary light | `primary-light` | `#3d9a7a` | Subtle accents |
| Primary 50 | `primary-50` | `#f0faf5` | Icon boxes, selected bg, outline hover |
| Secondary | `secondary` | `#D97706` | Orange accents (sparingly) |
| Secondary 50 | `secondary-50` | `#fff8ed` | Secondary icon boxes |
| Background | `bg-gray-50` | `#FAFAFA` | Page background (body) |
| Surface | `bg-white` | `#FFFFFF` | Cards, panels |
| Text primary | `text-gray-900` | `#1A1A1A` | Headings |
| Text body | `text-gray-500` | `#6B7280` | Body text |
| Text muted | `text-gray-400` | — | Timestamps, hints |
| Border | `border-gray-200` | `#E5E7EB` | Card/input borders |

**Never use `indigo-*` classes. Always use `primary` / `primary-dark` / `primary-50`.**

### Typography

- **Headings**: `font-extrabold tracking-tight text-gray-900`
- **Body text**: `text-sm text-gray-500 leading-relaxed`
- **Code/mono**: `font-mono` (JetBrains Mono via Tailwind config)
- **Font stack**: Inter, system-ui, sans-serif

### Spacing & Radius

- Card padding: `p-6` or `p-7`
- Card radius: `rounded-xl` (not `rounded-lg`)
- Button radius: `rounded-md`
- Section spacing: `space-y-6` or `space-y-8`

---

## Component Classes (defined in `app.css`)

### Buttons

| Class | When to use |
|-------|-------------|
| `.btn-primary` | Main CTA actions (Create, Save, Submit) |
| `.btn-outline` | Secondary actions alongside a primary CTA |
| `.btn-secondary` | Tertiary/ghost actions (Cancel, Duplicate) |
| `.btn-danger` | Destructive actions (Delete) |
| `.btn-sm` | Add to any button for small variant |

```html
<button class="btn-primary">Create Instance</button>
<button class="btn-primary btn-sm">Save</button>
<button class="btn-outline">View Demo</button>
<button class="btn-secondary">Cancel</button>
<button class="btn-danger btn-sm">Delete</button>
```

### Cards

| Class | When to use |
|-------|-------------|
| `.card` | Interactive cards (hover shows green border) |
| `.card-static` | Static containers (forms, info panels) |

```html
<div class="card p-6">Interactive card content</div>
<div class="card-static p-6">Form container</div>
```

### Other

| Class | Usage |
|-------|-------|
| `.page-title` | Page `<h1>` headings |
| `.page-subtitle` | Subtitle below page title |
| `.input` | Text inputs with primary focus ring |
| `.tab-active` | Active tab in tab bars |
| `.tab-inactive` | Inactive tab |
| `.link` | Inline links (primary color, hover darker) |
| `.spinner` | Loading spinners |

---

## Rules

1. **No indigo**: Use `primary` / `primary-dark` / `primary-50` everywhere
2. **Buttons**: Use `.btn-*` classes instead of hand-written Tailwind button styles
3. **Cards**: Use `rounded-xl` (not `rounded-lg`), add `hover:border-primary` for interactive cards
4. **Links**: Use `text-primary hover:text-primary-dark` or `.link` class
5. **Focus rings**: Use `focus:ring-primary focus:border-primary`
6. **Spinners**: Use `border-primary` (not `border-indigo-*` or `border-blue-*`)
7. **Selected states**: Use `border-primary bg-primary-50` (not indigo-50/500)
8. **Tab bars**: Active = `border-primary text-primary`, Inactive = `text-gray-500 hover:text-gray-700`
9. **Icon boxes**: Primary → `bg-primary-50` with `stroke="#2D7A5F"`, Secondary → `bg-secondary-50` with `stroke="#D97706"`
10. **No shadows on buttons**: Only subtle shadows on cards if needed (`shadow-sm` max)

---

## Checklist (verify after each page update)

- [ ] Zero `indigo-*` classes
- [ ] All primary action buttons use `.btn-primary`
- [ ] Cards use `rounded-xl` with appropriate hover
- [ ] Tabs use primary colors
- [ ] Focus rings use `focus:ring-primary`
- [ ] Spinners use `border-primary`
- [ ] Links use `text-primary`
- [ ] Page titles use `page-title` class or `text-2xl font-extrabold tracking-tight`
