# Wrappr — Owner Dashboard

The frontend for the Wrappr owner dashboard, built with Next.js 16, Tailwind v4, and shadcn/ui.

## Stack

- **Next.js 16** — App Router, TypeScript
- **Tailwind v4** — CSS-first config via `@import "tailwindcss"`
- **shadcn/ui** — base-nova style, CSS variables
- **TanStack Query v5** — server-state management
- **react-hook-form + zod** — form handling and validation

## Getting Started

```bash
# from repo root
make web-dev

# or directly
cd web && npm run dev
```

The app runs on **http://localhost:3001**.

## Environment

Copy `.env.example` to `.env.local` and fill in the values:

```bash
cp ../.env.example .env.local
```

| Variable | Description | Default |
|---|---|---|
| `NEXT_PUBLIC_API_URL` | Go API base URL | `http://localhost:8080` |

## Routes

| Route | Description |
|---|---|
| `/` | Redirects to `/dashboard` |
| `/dashboard` | Overview stats |
| `/dashboard/jobs` | Job listings |
| `/dashboard/team` | Team management |
| `/dashboard/settings` | Account settings |
| `/auth/login` | Sign in |

## Scripts

```bash
npm run dev      # dev server on :3001
npm run build    # production build
npm run lint     # ESLint
```
