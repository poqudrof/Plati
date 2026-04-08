# Landing Page — Ideas & Inspiration

Research on how private cloud / self-hosted dev platforms communicate their value.

---

## 1. Competitor Positioning

| Product | Headline style | Core angle |
|---------|---------------|------------|
| **Coder** | "Secure environments where devs and agents work in parallel" | Enterprise security, AI agent governance, SOC 2 / air-gapped |
| **Gitpod** | "Self-hosted, not self-managed" | Goldilocks between SaaS and self-hosted — security without ops burden |
| **DevPod** | "Open Source Dev-Environments-As-Code" | Client-only, no server, no vendor lock-in |
| **Coolify** | "Self-hosting with superpowers" | Open-source alternative to Vercel/Heroku, 60-80% cost savings |
| **Proxmox** | "Your Own Private Cloud" | Data sovereignty, KVM + LXC, install in < 1h |
| **Hetzner** | "Cloud-hosting for developers & teams" | No-nonsense, best price-performance in EU |

### Key takeaways for Plati
- Coder and Coolify lean into **bold cost claims** (90% VDI reduction, 60-80% savings)
- DevPod and Coolify use **"replaces X"** framing (name the products you beat)
- Gitpod coined a useful distinction: self-hosted != self-managed
- Proxmox proves that **"install in under 1 hour"** resonates for infra products

---

## 2. Messaging Themes (by importance)

### Tier 1 — Universal (everyone uses these)
1. **Sovereignty:** "Your infrastructure, your rules" / "Your data never leaves your servers"
2. **Speed:** "Minutes, not days" / "Ready to code in 90 seconds"
3. **No vendor lock-in:** Open standards, open source, portable templates
4. **Developer experience:** Consistency, reproducibility, one-click workflows

### Tier 2 — Strong differentiators
5. **Cost savings:** Concrete comparisons vs. public cloud (EUR/dev/month)
6. **Container isolation as security:** "If a container is compromised, delete it"
7. **AI-ready infrastructure:** Secure sandboxes for coding agents (2026 trend)
8. **Compliance:** SOC 2, GDPR, data residency — as a *consequence* of good architecture

### Tier 3 — Supporting
9. Open source transparency
10. Team collaboration / consistent tooling
11. Flexible infra (any cloud, bare metal, local)
12. Operational simplicity

---

## 3. Page Structure (best practices from 100+ dev tool landing pages)

Recommended order based on Evil Martians' research:

1. **Hero** — Centered headline + subheadline + product screenshot + dual CTA
2. **Trust bar** — GitHub stars, "Powered by Incus", "Secured by Tailscale"
3. **Problem statement** — Surface the pain ("Cloud envs cost too much. Local envs break. VDIs are stuck in 2010.")
4. **Feature showcase** — Bento grid or alternating image/text (chess layout)
5. **How it works** — 3-step visual flow
6. **Social proof** — Curated testimonials (NOT auto-pulled tweets)
7. **Comparison table** — Side-by-side vs. alternatives (optional)
8. **Pricing** — Keep simple if on main page
9. **Final CTA** — Full-width, visually distinct

### CTA best practices
- Specific > generic: "Deploy your first environment" beats "Get started"
- Always dual CTA in hero: one for conversion, one for exploration
- **15-minute rule:** developers expect value within 15 minutes of first click

---

## 4. Tailscale & Private Networking Messaging

Tailscale positions security as simplicity:
> "Making security hard to mess up means there's less mess to clean up."

Their 6 pillars: secure & private, identity-based, infrastructure-agnostic, mesh topology, resilient NAT traversal, "frustratingly easy" setup.

### How Plati should frame Tailscale integration
- "Every environment joins your private mesh network automatically"
- "No port forwarding. No public IPs. No VPN clients to configure"
- "One-click app deployment accessible only on your private network"
- Frame it as zero-config networking, not as a VPN replacement

### 2026 AI trend
Tailscale now positions "Securing AI" as a product pillar — private networking for AI agents is becoming mainstream. Plati can ride this wave.

---

## 5. Concrete Ideas for Plati

### Headline candidates
| Option | Angle |
|--------|-------|
| "Your private cloud for dev environments. On your hardware. In minutes." | Sovereignty + speed |
| "Dev environments that live on your servers, not someone else's." | Bold, opinionated |
| "Self-hosted dev environments with superpowers." | Proven pattern (Coolify) |
| "Private cloud for developers. No AWS bill required." | Cost + sovereignty |
| "Spin up isolated dev environments on your own infrastructure." | Descriptive, clear |

### Subheadline draft
> Plati turns your bare-metal servers into a developer platform with container isolation, persistent workspaces, private networking via Tailscale, and one-click environment templates. Open source. Self-hosted. Powered by Incus.

### Positioning statement (internal)
> Plati is a self-hosted platform that turns bare-metal servers into a private developer cloud. Built on Incus for strong container isolation and Tailscale for zero-config private networking, Plati gives teams cloud-grade developer environments at a fraction of the cost — without sending a single line of code to a third party.

---

## 6. New Sections to Add

### Cost comparison table
Simple, concrete numbers:

| | Plati + Hetzner AX42 | GitHub Codespaces | Gitpod |
|-|----------------------|-------------------|--------|
| Price/dev/month | ~5 EUR (10 devs on 1 server) | ~50 EUR | ~35 EUR |
| Data location | Your server | Microsoft US | Google EU |
| GPU access | Native passthrough | Limited | No |
| AI agent sandboxes | Included | N/A | N/A |

### AI-ready callout section
"Safe sandboxes for AI coding agents. Every agent runs in its own isolated container with controlled access." — Rides the 2026 wave that Coder is capitalizing on.

### Incus advantage section
Incus system containers are more isolated than Docker (full systemd, separate network namespace, AppArmor profiles). This is a genuine differentiator worth calling out vs. Docker-based competitors.

### "Replaces" framing
Explicitly name what Plati replaces: "Stop paying for Codespaces. Stop fighting with local Docker setups. Stop maintaining brittle VDIs."

---

## 7. Anti-patterns to Avoid

- Generic "Get started" CTAs without specificity
- Feature lists without problem context
- Auto-pulled social media testimonials
- Overly complex animations/interactions
- Broad claims without quantification
- Compliance jargon as a primary message (lead with practical benefits instead)

---

## Sources

- [Evil Martians: 100 Dev Tool Landing Pages](https://evilmartians.com/chronicles/we-studied-100-devtool-landing-pages-here-is-what-actually-works-in-2025)
- [Coder Value Proposition](https://www.baytechconsulting.com/blog/coder-com-platform-value-proposition-2025)
- [Gitpod: Self-hosted, not self-managed](https://www.gitpod.io/blog/self-hosted-not-self-managed)
- [Coolify Landing Page](https://coolify.io/)
- [Tailscale: Why Tailscale](https://tailscale.com/why-tailscale)
- [BCG: Sovereign Clouds](https://www.bcg.com/publications/2025/sovereign-clouds-reshaping-national-data-security)
- [Private vs Public Cloud Cost Tipping Points](https://openmetal.io/resources/blog/public-cloud-vs-private-cloud-cost-tipping-points/)
