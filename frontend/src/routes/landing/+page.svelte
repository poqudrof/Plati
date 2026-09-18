<style>
  :global(html) {
    scroll-behavior: smooth;
  }

  @keyframes pageFadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes blink {
    50% { opacity: 0; }
  }

  .page-wrapper {
    animation: pageFadeIn 0.4s ease-out;
    background: #FAFAFA;
  }

  :global(.reveal) {
    opacity: 0;
    transform: translateY(20px);
    transition: opacity 0.4s ease-out, transform 0.4s ease-out;
  }
  :global(.reveal.visible) { opacity: 1; transform: translateY(0); }
  :global(.reveal-d1) { transition-delay: 0.1s; }
  :global(.reveal-d2) { transition-delay: 0.2s; }
  :global(.reveal-d3) { transition-delay: 0.3s; }
  :global(.reveal-d4) { transition-delay: 0.4s; }
  :global(.reveal-d5) { transition-delay: 0.5s; }

  :global(.card) {
    background: white;
    border: 1px solid #E5E7EB;
    border-radius: 0.75rem;
    transition: border-color 0.2s ease;
  }
  :global(.card:hover) { border-color: #2D7A5F; }

  :global(.btn-primary) {
    display: inline-flex; align-items: center; gap: 0.4rem;
    padding: 0.75rem 1.5rem; border-radius: 0.375rem;
    background: #2D7A5F; color: white;
    border: 1px solid #2D7A5F;
    font-weight: 600; font-size: 0.9375rem;
    text-decoration: none; cursor: pointer;
    transition: background 0.2s ease;
    white-space: nowrap;
  }
  :global(.btn-primary:hover) { background: #235f4a; border-color: #235f4a; }
  :global(.btn-primary:active) { opacity: 0.9; }

  :global(.btn-outline) {
    display: inline-flex; align-items: center; gap: 0.4rem;
    padding: 0.75rem 1.5rem; border-radius: 0.375rem;
    background: transparent; color: #2D7A5F;
    border: 1px solid #2D7A5F;
    font-weight: 600; font-size: 0.9375rem;
    text-decoration: none; cursor: pointer;
    transition: background 0.2s ease;
    white-space: nowrap;
  }
  :global(.btn-outline:hover) { background: #f0faf5; }

  :global(.blink) { animation: blink 1s step-end infinite; }

  :global(.step-line) {
    width: 2px; background: #E5E7EB; flex: 1; margin-top: 6px;
  }
</style>

<script>
  import { onMount } from 'svelte';

  let mobileMenuOpen = $state(false);
  let navbarScrolled = $state(false);

  function toggleMenu() {
    mobileMenuOpen = !mobileMenuOpen;
  }

  function closeMenu() {
    mobileMenuOpen = false;
  }

  onMount(() => {
    const observer = new IntersectionObserver((entries) => {
      entries.forEach(e => {
        if (e.isIntersecting) {
          e.target.classList.add('visible');
          observer.unobserve(e.target);
        }
      });
    }, { threshold: 0.08, rootMargin: '0px 0px -30px 0px' });

    document.querySelectorAll('.reveal').forEach(el => observer.observe(el));

    const handleScroll = () => {
      navbarScrolled = window.scrollY > 24;
    };
    window.addEventListener('scroll', handleScroll, { passive: true });

    const handleResize = () => {
      if (window.innerWidth >= 768) mobileMenuOpen = false;
    };
    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('scroll', handleScroll);
      window.removeEventListener('resize', handleResize);
      observer.disconnect();
    };
  });
</script>

<div class="page-wrapper font-sans text-gray-900 antialiased">

  <!-- ======================== NAVBAR ======================== -->
  <nav
    class="fixed top-0 inset-x-0 z-50"
    style:background={navbarScrolled ? 'white' : 'transparent'}
    style:box-shadow={navbarScrolled ? '0 1px 4px rgba(0,0,0,0.08)' : 'none'}
    style:transition="background 0.25s ease, box-shadow 0.25s ease"
  >
    <div class="max-w-6xl mx-auto px-6">
      <div class="flex items-center justify-between h-16">

        <!-- Logo -->
        <a href="#" class="flex items-center gap-2 no-underline">
          <svg width="30" height="30" viewBox="0 0 30 30" fill="none">
            <rect width="30" height="30" rx="8" fill="#2D7A5F"/>
            <path d="M9 21C9 21 11 13 17 11C23 9 21 15 17 15C13 15 15 21 15 21"
              stroke="white" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <circle cx="21" cy="10" r="2.5" fill="#D97706"/>
          </svg>
          <span class="font-extrabold text-xl tracking-tight text-gray-900">plati</span>
        </a>

        <!-- Desktop links -->
        <div class="hidden md:flex items-center gap-8">
          <a href="#fonctionnalites" class="text-sm font-medium text-gray-500 hover:text-primary transition-colors no-underline">Fonctionnalites</a>
          <a href="#vibe-coders" class="text-sm font-medium text-gray-500 hover:text-primary transition-colors no-underline">Vibe coders</a>
          <a href="#developpeurs" class="text-sm font-medium text-gray-500 hover:text-primary transition-colors no-underline">Developpeurs</a>
          <a href="#entreprises" class="text-sm font-medium text-gray-500 hover:text-primary transition-colors no-underline">Entreprises</a>
        </div>

        <!-- CTA + hamburger -->
        <div class="flex items-center gap-3">
          <a href="#cta" class="btn-primary hidden md:inline-flex text-sm py-2 px-4">Deployer Plati</a>
          <button onclick={toggleMenu} class="md:hidden p-1 border-0 bg-transparent cursor-pointer">
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#374151" stroke-width="2" stroke-linecap="round">
              <line x1="3" y1="6" x2="21" y2="6"/>
              <line x1="3" y1="12" x2="21" y2="12"/>
              <line x1="3" y1="18" x2="21" y2="18"/>
            </svg>
          </button>
        </div>
      </div>
    </div>

    <!-- Mobile menu -->
    {#if mobileMenuOpen}
    <div class="bg-white border-t border-gray-100 px-6 py-4 flex flex-col gap-3">
      <a href="#fonctionnalites" onclick={closeMenu} class="block py-2 text-gray-700 font-medium border-b border-gray-50 no-underline">Fonctionnalites</a>
      <a href="#vibe-coders" onclick={closeMenu} class="block py-2 text-gray-700 font-medium border-b border-gray-50 no-underline">Vibe coders</a>
      <a href="#developpeurs" onclick={closeMenu} class="block py-2 text-gray-700 font-medium border-b border-gray-50 no-underline">Developpeurs</a>
      <a href="#entreprises" onclick={closeMenu} class="block py-2 text-gray-700 font-medium border-b border-gray-50 no-underline">Entreprises</a>
      <a href="#cta" onclick={closeMenu} class="btn-primary mt-2 justify-center">Deployer Plati</a>
    </div>
    {/if}
  </nav>

  <!-- ======================== HERO ======================== -->
  <section id="hero" class="min-h-screen flex items-center pt-16" style="padding-bottom:4rem;">
    <div class="max-w-6xl mx-auto px-6 w-full">
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-12 items-center">

        <!-- Text -->
        <div>
          <!-- Badge -->
          <div class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 mb-6"
               style="background:#f0faf5; border-color:#b7e0d0;">
            <div class="w-1.5 h-1.5 rounded-full" style="background:#2D7A5F;"></div>
            <span class="text-xs font-semibold" style="color:#2D7A5F;">Open source &middot; Self-hosted &middot; Cloud prive</span>
          </div>

          <h1 class="font-extrabold leading-none tracking-tight text-gray-900 mb-5"
              style="font-size:clamp(2.25rem,5vw,3.75rem); letter-spacing:-0.03em; line-height:1.08;">
            Vos Codespaces.<br>
            Sur vos serveurs.<br>
            <span style="color:#2D7A5F;">Sans limites.</span>
          </h1>

          <p class="text-gray-500 mb-8 leading-relaxed"
             style="font-size:clamp(1rem,2vw,1.125rem); max-width:520px; line-height:1.7;">
            Plati vous donne des VMs de dev isolees comme des Codespaces, mais hebergees chez vous.
            Un espace par projet, sans surcout. Reseau prive Tailscale integre.
          </p>

          <div class="flex flex-wrap gap-3 mb-8">
            <a href="#cta" class="btn-primary" style="font-size:1rem; padding:0.875rem 1.75rem;">
              Deployer Plati
            </a>
            <a href="#how-it-works" class="btn-outline" style="font-size:1rem; padding:0.875rem 1.75rem;">
              Voir comment ca marche
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                   stroke-linecap="round" stroke-linejoin="round">
                <line x1="5" y1="12" x2="19" y2="12"/>
                <polyline points="12 5 19 12 12 19"/>
              </svg>
            </a>
          </div>

          <!-- Trust signals -->
          <div class="flex flex-wrap gap-5">
            <span class="flex items-center gap-1.5 text-sm text-gray-400">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="2.5"
                   stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              1 VM par projet, sans surcout
            </span>
            <span class="flex items-center gap-1.5 text-sm text-gray-400">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="2.5"
                   stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              Self-hosted &middot; Open source
            </span>
            <span class="flex items-center gap-1.5 text-sm text-gray-400">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="2.5"
                   stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              Installation en &lt; 1h
            </span>
          </div>
        </div>

        <!-- Terminal mockup -->
        <div class="relative">
          <div class="rounded-xl overflow-hidden font-mono" style="background:#1e1e2e; box-shadow:0 20px 60px rgba(0,0,0,0.18);">
            <!-- Title bar -->
            <div class="flex items-center gap-1.5 px-4 py-3" style="background:#2a2a3d; border-bottom:1px solid rgba(255,255,255,0.05);">
              <div class="w-3 h-3 rounded-full" style="background:#ff5f57;"></div>
              <div class="w-3 h-3 rounded-full" style="background:#febc2e;"></div>
              <div class="w-3 h-3 rounded-full" style="background:#28c840;"></div>
              <span class="ml-3 text-xs" style="color:#6b7280; font-family:'JetBrains Mono',monospace;">plati — terminal</span>
            </div>
            <!-- Content -->
            <div class="p-6" style="font-size:0.8125rem; line-height:1.85; font-family:'JetBrains Mono',monospace;">
              <div>
                <span style="color:#6b7280;">$</span>
                <span style="color:#a6e3a1;"> plati</span>
                <span style="color:#cdd6f4;"> create</span>
                <span style="color:#f5c2e7;"> --template</span>
                <span style="color:#fab387;"> fullstack</span>
                <span style="color:#cdd6f4;"> mon-projet</span>
              </div>
              <div style="color:#6b7280; margin-top:0.5rem;">&#x27F3; &nbsp;Provisioning environment...</div>
              <div style="color:#a6e3a1;">&#x2713; &nbsp;<span style="color:#cdd6f4;">VM created</span> <span style="color:#6b7280;">(2s)</span></div>
              <div style="color:#a6e3a1;">&#x2713; &nbsp;<span style="color:#cdd6f4;">Tailscale connected</span> <span style="color:#6b7280;">(3s)</span></div>
              <div style="color:#a6e3a1;">&#x2713; &nbsp;<span style="color:#cdd6f4;">Git repo mounted</span> <span style="color:#6b7280;">(1s)</span></div>
              <div style="color:#a6e3a1;">&#x2713; &nbsp;<span style="color:#cdd6f4;">VS Code Server ready</span> <span style="color:#6b7280;">(4s)</span></div>
              <div style="margin-top:0.75rem; color:#89b4fa;">&#x2192; &nbsp;<span style="text-decoration:underline;">https://mon-projet.tail1234.ts.net</span></div>
              <div style="color:#a6e3a1; font-weight:600;">&#x2713; &nbsp;Pret en 10 secondes</div>
              <div style="margin-top:0.5rem;">
                <span style="color:#6b7280;">$</span>
                <span class="blink" style="color:#cdd6f4;"> &#x25CB;</span>
              </div>
            </div>
          </div>

          <!-- Floating badges -->
          <div class="absolute -bottom-4 -right-2 flex items-center gap-2 bg-white rounded-lg px-3 py-2.5"
               style="border:1px solid #E5E7EB; box-shadow:0 4px 14px rgba(0,0,0,0.07);">
            <div class="w-2 h-2 rounded-full" style="background:#22c55e; flex-shrink:0;"></div>
            <span class="text-sm font-semibold text-gray-900">Reseau prive</span>
          </div>
          <div class="absolute -top-3 -left-2 flex items-center gap-2 bg-white rounded-lg px-3 py-2.5"
               style="border:1px solid #E5E7EB; box-shadow:0 4px 14px rgba(0,0,0,0.07);">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <rect width="18" height="11" x="3" y="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
            <span class="text-sm font-semibold text-gray-900">On-premise</span>
          </div>
        </div>

      </div>
    </div>
  </section>

  <!-- ======================== PROBLEM STATEMENT ======================== -->
  <section class="py-16" style="background:#1e1e2e;">
    <div class="max-w-5xl mx-auto px-6">
      <div class="reveal text-center">
        <p class="text-sm font-semibold tracking-wide uppercase mb-4" style="color:#D97706;">Le probleme</p>
        <h2 class="font-extrabold tracking-tight mb-6"
            style="font-size:clamp(1.5rem,3vw,2.25rem); letter-spacing:-0.02em; color:white;">
          Codespaces facture chaque heure de chaque VM.<br class="hidden sm:block">
          En local, tout casse des qu'on change de projet.
        </h2>
        <p style="font-size:1.0625rem; line-height:1.65; color:#9ca3af; max-width:600px;" class="mx-auto">
          Avec Plati, creez autant de VMs que de projets sur vos propres serveurs.
          Isolation totale, zero surcout, onboarding instantane.
        </p>
      </div>
    </div>
  </section>

  <!-- ======================== FONCTIONNALITES CLES ======================== -->
  <section id="fonctionnalites" class="py-20 bg-white" style="border-top:1px solid #F3F4F6;">
    <div class="max-w-6xl mx-auto px-6">

      <div class="reveal text-center mb-14">
        <div class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 mb-4"
             style="background:#f0faf5; border-color:#b7e0d0;">
          <span class="text-xs font-semibold" style="color:#2D7A5F;">Fonctionnalites cles</span>
        </div>
        <h2 class="font-extrabold tracking-tight text-gray-900 mb-3"
            style="font-size:clamp(1.75rem,3.5vw,2.5rem); letter-spacing:-0.02em;">
          Tout ce qu'il faut, rien de superflu
        </h2>
        <p class="text-gray-500 mx-auto" style="font-size:1.0625rem; max-width:600px; line-height:1.65;">
          De la creation d'environnement a la gestion des acces, Plati couvre
          le cycle de vie complet de vos VMs de dev.
        </p>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-6">

        <div class="card reveal reveal-d1 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#f0faf5;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">Environnements prets en un clic</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Choisissez un template, Plati provisionne une VM Incus isolee : repo clone,
            outils installes, cles SSH injectees, ressources configurees. Reproductible a l'infini.
          </p>
        </div>

        <div class="card reveal reveal-d2 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#f0faf5;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/>
              <polyline points="14 2 14 8 20 8"/>
              <circle cx="10" cy="15" r="2"/>
              <path d="M12 13.5V17"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">Fichiers et snapshots integres</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Parcourez, telechargez et snapshotez vos volumes persistants directement
            depuis l'interface web : navigateur de fichiers, export en un clic,
            restauration de snapshot sans jamais ouvrir un terminal SSH.
          </p>
        </div>

        <div class="card reveal reveal-d3 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#f0faf5;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <rect width="18" height="11" x="3" y="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">Acces et identites maitrises</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            SSO Entra ID ou cles API pour l'automatisation, et un choix par utilisateur
            entre cle SSH/Tailscale geree par l'admin ou cles personnelles — bascule
            possible instance par instance. Self-service, sans perdre le controle.
          </p>
        </div>

      </div>
    </div>
  </section>

  <!-- ======================== SECTION 1: VIBE CODERS ======================== -->
  <section id="vibe-coders" class="py-20" style="background:#FAFAFA; border-top:1px solid #F3F4F6;">
    <div class="max-w-6xl mx-auto px-6">

      <div class="reveal text-center mb-14">
        <div class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 mb-4"
             style="background:#fff8ed; border-color:#fde68a;">
          <span class="text-xs font-semibold" style="color:#D97706;">01</span>
        </div>
        <h2 class="font-extrabold tracking-tight text-gray-900 mb-3"
            style="font-size:clamp(1.75rem,3.5vw,2.5rem); letter-spacing:-0.02em;">
          Pour les <span style="color:#D97706;">vibe coders</span>
        </h2>
        <p class="text-gray-500 mx-auto" style="font-size:1.0625rem; max-width:560px; line-height:1.65;">
          Une VM dediee par projet, prete en secondes. Rien a installer sur votre machine.
          L'IA est deja la, vous codez directement dans le navigateur.
        </p>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-6">

        <div class="card reveal reveal-d1 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#fff8ed;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">Un projet = une VM</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Chaque projet tourne dans sa propre VM isolee. Pas de conflits entre projets,
            pas de dependances qui cassent. Creez-en autant que vous voulez, sans surcout.
          </p>
        </div>

        <div class="card reveal reveal-d2 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#fff8ed;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <rect width="18" height="10" x="3" y="11" rx="2"/>
              <circle cx="12" cy="5" r="2"/>
              <path d="M12 7v4"/>
              <line x1="8" x2="8" y1="16" y2="16"/>
              <line x1="16" x2="16" y1="16" y2="16"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">L'IA code pour vous</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Claude Code, Cursor ou Copilot pre-installes. Decrivez ce que vous voulez,
            l'IA genere le code dans un environnement pret a l'emploi.
          </p>
        </div>

        <div class="card reveal reveal-d3 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#fff8ed;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="2"/>
              <path d="M12 2v4"/><path d="M12 18v4"/>
              <path d="M4.93 4.93l2.83 2.83"/><path d="M16.24 16.24l2.83 2.83"/>
              <path d="M2 12h4"/><path d="M18 12h4"/>
              <path d="M4.93 19.07l2.83-2.83"/><path d="M16.24 7.76l2.83-2.83"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">1 clic pour tester votre appli</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Nouveau projet ? Un clic et c'est pret. L'appli tourne et elle est
            accessible immediatement sur votre reseau prive pour la tester et la montrer.
          </p>
        </div>

      </div>
    </div>
  </section>

  <!-- ======================== SECTION 2: DEVELOPPEURS ======================== -->
  <section id="developpeurs" class="py-20 bg-white" style="border-top:1px solid #F3F4F6;">
    <div class="max-w-6xl mx-auto px-6">

      <div class="reveal text-center mb-14">
        <div class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 mb-4"
             style="background:#f0faf5; border-color:#b7e0d0;">
          <span class="text-xs font-semibold" style="color:#2D7A5F;">02</span>
        </div>
        <h2 class="font-extrabold tracking-tight text-gray-900 mb-3"
            style="font-size:clamp(1.75rem,3.5vw,2.5rem); letter-spacing:-0.02em;">
          Pour les <span style="color:#2D7A5F;">developpeurs</span>
        </h2>
        <p class="text-gray-500 mx-auto" style="font-size:1.0625rem; max-width:560px; line-height:1.65;">
          Des workflows reutilisables pour toute l'equipe.
          Templates, mixins, volumes persistants — construisez une fois, utilisez partout.
        </p>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-6">

        <div class="card reveal reveal-d1 p-8">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 flex items-center justify-center rounded-lg flex-shrink-0" style="background:#f0faf5;">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <rect x="2" y="2" width="8" height="8" rx="1"/>
                <rect x="14" y="2" width="8" height="8" rx="1"/>
                <rect x="2" y="14" width="8" height="8" rx="1"/>
                <rect x="14" y="14" width="8" height="8" rx="1"/>
              </svg>
            </div>
            <h3 class="font-bold text-gray-900">Templates YAML</h3>
          </div>
          <p class="text-gray-500 leading-relaxed mb-4" style="font-size:0.9375rem;">
            Decrivez vos stacks en YAML : langages, outils, repos Git.
            Un template = un environnement reproductible a l'infini. Plus de "ca marche chez moi".
          </p>
          <div class="flex flex-wrap gap-2">
            <span class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium" style="background:#f0faf5; color:#2D7A5F;">React + Node</span>
            <span class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium" style="background:#f0faf5; color:#2D7A5F;">Python + FastAPI</span>
            <span class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium" style="background:#f0faf5; color:#2D7A5F;">Go + Svelte</span>
          </div>
        </div>

        <div class="card reveal reveal-d2 p-8">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 flex items-center justify-center rounded-lg flex-shrink-0" style="background:#f0faf5;">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
              </svg>
            </div>
            <h3 class="font-bold text-gray-900">Mixins composables</h3>
          </div>
          <p class="text-gray-500 leading-relaxed mb-4" style="font-size:0.9375rem;">
            Ajoutez Tailscale, VS Code Server, Claude Code ou SSHX a n'importe quel template via des mixins.
            Composez votre stack comme des briques.
          </p>
          <div class="flex flex-wrap gap-2">
            <span class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium" style="background:#f0faf5; color:#2D7A5F;">tailscale.yaml</span>
            <span class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium" style="background:#f0faf5; color:#2D7A5F;">claude-code.yaml</span>
            <span class="inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium" style="background:#f0faf5; color:#2D7A5F;">sshx.yaml</span>
          </div>
        </div>

        <div class="card reveal reveal-d3 p-8">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 flex items-center justify-center rounded-lg flex-shrink-0" style="background:#f0faf5;">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/>
                <polyline points="14 2 14 8 20 8"/>
              </svg>
            </div>
            <h3 class="font-bold text-gray-900">Volumes persistants</h3>
          </div>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Vos workspaces survivent aux rebuilds. Repos Git pre-caches sur le serveur
            pour un demarrage instantane, meme sur des monorepos lourds.
          </p>
        </div>

        <div class="card reveal reveal-d4 p-8">
          <div class="flex items-center gap-3 mb-4">
            <div class="w-10 h-10 flex items-center justify-center rounded-lg flex-shrink-0" style="background:#f0faf5;">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="16 18 22 12 16 6"/>
                <polyline points="8 6 2 12 8 18"/>
              </svg>
            </div>
            <h3 class="font-bold text-gray-900">Outils integres</h3>
          </div>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            VS Code Server dans le navigateur, acces SSH natif, terminal partage via SSHX
            pour le pair programming. Tout fonctionne sans configuration.
          </p>
        </div>

      </div>
    </div>
  </section>

  <!-- ======================== SECTION 3: ENTREPRISES ======================== -->
  <section id="entreprises" class="py-20" style="background:#FAFAFA; border-top:1px solid #F3F4F6;">
    <div class="max-w-6xl mx-auto px-6">

      <div class="reveal text-center mb-14">
        <div class="inline-flex items-center gap-2 rounded-full border px-3 py-1.5 mb-4"
             style="background:#1e1e2e; border-color:#3a3a4d;">
          <span class="text-xs font-semibold" style="color:white;">03</span>
        </div>
        <h2 class="font-extrabold tracking-tight text-gray-900 mb-3"
            style="font-size:clamp(1.75rem,3.5vw,2.5rem); letter-spacing:-0.02em;">
          Pour les <span style="color:#2D7A5F;">entreprises</span> &amp; DevOps
        </h2>
        <p class="text-gray-500 mx-auto" style="font-size:1.0625rem; max-width:600px; line-height:1.65;">
          Cloud prive, controle total des donnees. Vos VMs, vos applis et vos agents IA
          tournent sur vos serveurs et communiquent via un reseau prive chiffre.
        </p>
      </div>

      <!-- Architecture SVG diagram -->
      <div class="reveal mb-8">
        <svg viewBox="0 0 800 420" fill="none" xmlns="http://www.w3.org/2000/svg" class="w-full" style="max-width:800px; margin:0 auto; display:block;">
          <!-- Background -->
          <rect width="800" height="420" rx="16" fill="#1e1e2e"/>

          <!-- ===== LEFT: Server infrastructure ===== -->
          <rect x="30" y="50" width="240" height="320" rx="12" fill="#2a2a3d" stroke="#3a3a4d" stroke-width="1"/>
          <text x="150" y="82" text-anchor="middle" fill="#9ca3af" font-size="11" font-weight="600" letter-spacing="1.5" font-family="system-ui">VOS SERVEURS</text>

          <!-- Server icon -->
          <rect x="60" y="100" width="180" height="36" rx="6" fill="#2D7A5F" fill-opacity="0.15" stroke="#2D7A5F" stroke-width="1"/>
          <circle cx="80" cy="118" r="4" fill="#2D7A5F"/>
          <text x="96" y="123" fill="#a6e3a1" font-size="12" font-weight="600" font-family="system-ui">Plati (orchestration)</text>

          <!-- VM boxes -->
          <rect x="60" y="150" width="84" height="64" rx="6" fill="#2a2a3d" stroke="#4a4a5d" stroke-width="1"/>
          <rect x="66" y="156" width="20" height="14" rx="3" fill="#2D7A5F" fill-opacity="0.3"/>
          <rect x="66" y="156" width="20" height="14" rx="3" stroke="#2D7A5F" stroke-width="0.8"/>
          <text x="102" y="168" text-anchor="middle" fill="#cdd6f4" font-size="10" font-weight="600" font-family="system-ui">VM 1</text>
          <text x="102" y="182" text-anchor="middle" fill="#6b7280" font-size="9" font-family="system-ui">Projet A</text>
          <text x="102" y="204" text-anchor="middle" fill="#6b7280" font-size="8" font-family="system-ui">VS Code + SSH</text>

          <rect x="156" y="150" width="84" height="64" rx="6" fill="#2a2a3d" stroke="#4a4a5d" stroke-width="1"/>
          <rect x="162" y="156" width="20" height="14" rx="3" fill="#D97706" fill-opacity="0.3"/>
          <rect x="162" y="156" width="20" height="14" rx="3" stroke="#D97706" stroke-width="0.8"/>
          <text x="198" y="168" text-anchor="middle" fill="#cdd6f4" font-size="10" font-weight="600" font-family="system-ui">VM 2</text>
          <text x="198" y="182" text-anchor="middle" fill="#6b7280" font-size="9" font-family="system-ui">Projet B</text>
          <text x="198" y="204" text-anchor="middle" fill="#6b7280" font-size="8" font-family="system-ui">Claude Code</text>

          <rect x="60" y="224" width="84" height="64" rx="6" fill="#2a2a3d" stroke="#4a4a5d" stroke-width="1"/>
          <rect x="66" y="230" width="20" height="14" rx="3" fill="#2D7A5F" fill-opacity="0.3"/>
          <rect x="66" y="230" width="20" height="14" rx="3" stroke="#2D7A5F" stroke-width="0.8"/>
          <text x="102" y="242" text-anchor="middle" fill="#cdd6f4" font-size="10" font-weight="600" font-family="system-ui">VM 3</text>
          <text x="102" y="256" text-anchor="middle" fill="#6b7280" font-size="9" font-family="system-ui">Projet C</text>
          <text x="102" y="278" text-anchor="middle" fill="#6b7280" font-size="8" font-family="system-ui">API + DB</text>

          <rect x="156" y="224" width="84" height="64" rx="6" fill="#2a2a3d" stroke="#4a4a5d" stroke-width="1" stroke-dasharray="4 3"/>
          <text x="198" y="257" text-anchor="middle" fill="#4a4a5d" font-size="20" font-family="system-ui">+</text>
          <text x="198" y="278" text-anchor="middle" fill="#4a4a5d" font-size="8" font-family="system-ui">Sans surcout</text>

          <!-- Storage -->
          <rect x="60" y="300" width="180" height="28" rx="6" fill="#2a2a3d" stroke="#4a4a5d" stroke-width="1"/>
          <rect x="66" y="306" width="8" height="16" rx="2" fill="#2D7A5F" fill-opacity="0.4"/>
          <rect x="78" y="306" width="8" height="16" rx="2" fill="#2D7A5F" fill-opacity="0.3"/>
          <rect x="90" y="306" width="8" height="16" rx="2" fill="#2D7A5F" fill-opacity="0.2"/>
          <text x="160" y="319" text-anchor="middle" fill="#6b7280" font-size="9" font-family="system-ui">Volumes + Git cache</text>

          <!-- ===== CENTER: Tailscale mesh ===== -->
          <!-- Connection lines from server to Tailscale -->
          <line x1="270" y1="180" x2="340" y2="180" stroke="#2D7A5F" stroke-width="1.5" stroke-dasharray="6 3"/>
          <line x1="270" y1="250" x2="340" y2="250" stroke="#2D7A5F" stroke-width="1.5" stroke-dasharray="6 3"/>

          <!-- Tailscale cloud -->
          <rect x="340" y="130" width="140" height="160" rx="12" fill="#2D7A5F" fill-opacity="0.08" stroke="#2D7A5F" stroke-width="1.5"/>
          <text x="410" y="158" text-anchor="middle" fill="#2D7A5F" font-size="11" font-weight="700" letter-spacing="0.5" font-family="system-ui">TAILSCALE</text>
          <text x="410" y="172" text-anchor="middle" fill="#2D7A5F" font-size="9" font-family="system-ui" opacity="0.7">Reseau prive mesh</text>

          <!-- Mesh lines inside Tailscale -->
          <circle cx="380" cy="210" r="5" fill="#2D7A5F" fill-opacity="0.5"/>
          <circle cx="410" cy="230" r="5" fill="#2D7A5F" fill-opacity="0.5"/>
          <circle cx="440" cy="210" r="5" fill="#2D7A5F" fill-opacity="0.5"/>
          <circle cx="395" cy="250" r="5" fill="#2D7A5F" fill-opacity="0.5"/>
          <circle cx="425" cy="250" r="5" fill="#2D7A5F" fill-opacity="0.5"/>
          <line x1="380" y1="210" x2="410" y2="230" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="410" y1="230" x2="440" y2="210" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="380" y1="210" x2="440" y2="210" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="380" y1="210" x2="395" y2="250" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="440" y1="210" x2="425" y2="250" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="395" y1="250" x2="425" y2="250" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="395" y1="250" x2="410" y2="230" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>
          <line x1="425" y1="250" x2="410" y2="230" stroke="#2D7A5F" stroke-width="0.8" opacity="0.4"/>

          <!-- WireGuard label -->
          <rect x="368" y="270" width="84" height="18" rx="4" fill="#2D7A5F" fill-opacity="0.15"/>
          <text x="410" y="283" text-anchor="middle" fill="#2D7A5F" font-size="8" font-weight="600" font-family="system-ui">WireGuard chiffre</text>

          <!-- Connection lines from Tailscale to right side -->
          <line x1="480" y1="185" x2="540" y2="116" stroke="#D97706" stroke-width="1.5" stroke-dasharray="6 3"/>
          <line x1="480" y1="210" x2="540" y2="230" stroke="#6b7280" stroke-width="1.5" stroke-dasharray="6 3"/>
          <line x1="480" y1="260" x2="540" y2="340" stroke="#89b4fa" stroke-width="1.5" stroke-dasharray="6 3"/>

          <!-- ===== RIGHT TOP: Users ===== -->
          <rect x="540" y="56" width="230" height="120" rx="12" fill="#2a2a3d" stroke="#3a3a4d" stroke-width="1"/>
          <text x="655" y="80" text-anchor="middle" fill="#9ca3af" font-size="11" font-weight="600" letter-spacing="1.5" font-family="system-ui">UTILISATEURS</text>

          <!-- User 1: laptop -->
          <rect x="560" y="90" width="60" height="54" rx="6" fill="#2a2a3d" stroke="#D97706" stroke-width="1"/>
          <rect x="566" y="96" width="48" height="22" rx="3" fill="#D97706" fill-opacity="0.1"/>
          <text x="590" y="111" text-anchor="middle" fill="#D97706" font-size="8" font-weight="600" font-family="system-ui">Navigateur</text>
          <text x="590" y="136" text-anchor="middle" fill="#6b7280" font-size="7" font-family="system-ui">Vibe coder</text>

          <!-- User 2: laptop -->
          <rect x="632" y="90" width="60" height="54" rx="6" fill="#2a2a3d" stroke="#D97706" stroke-width="1"/>
          <rect x="638" y="96" width="48" height="22" rx="3" fill="#D97706" fill-opacity="0.1"/>
          <text x="662" y="111" text-anchor="middle" fill="#D97706" font-size="8" font-weight="600" font-family="system-ui">VS Code</text>
          <text x="662" y="136" text-anchor="middle" fill="#6b7280" font-size="7" font-family="system-ui">Developpeur</text>

          <!-- User 3: terminal -->
          <rect x="704" y="90" width="54" height="54" rx="6" fill="#2a2a3d" stroke="#D97706" stroke-width="1"/>
          <rect x="710" y="96" width="42" height="22" rx="3" fill="#D97706" fill-opacity="0.1"/>
          <text x="731" y="111" text-anchor="middle" fill="#D97706" font-size="8" font-weight="600" font-family="system-ui">SSH</text>
          <text x="731" y="136" text-anchor="middle" fill="#6b7280" font-size="7" font-family="system-ui">DevOps</text>

          <!-- ===== RIGHT MIDDLE: Apps ===== -->
          <rect x="540" y="190" width="230" height="80" rx="12" fill="#2a2a3d" stroke="#3a3a4d" stroke-width="1"/>
          <text x="655" y="214" text-anchor="middle" fill="#9ca3af" font-size="11" font-weight="600" letter-spacing="1.5" font-family="system-ui">APPLIS DEPLOYEES</text>

          <rect x="560" y="226" width="62" height="30" rx="5" fill="#2a2a3d" stroke="#6b7280" stroke-width="0.8"/>
          <text x="591" y="245" text-anchor="middle" fill="#cdd6f4" font-size="9" font-family="system-ui">API interne</text>

          <rect x="630" y="226" width="62" height="30" rx="5" fill="#2a2a3d" stroke="#6b7280" stroke-width="0.8"/>
          <text x="661" y="245" text-anchor="middle" fill="#cdd6f4" font-size="9" font-family="system-ui">Dashboard</text>

          <rect x="700" y="226" width="56" height="30" rx="5" fill="#2a2a3d" stroke="#6b7280" stroke-width="0.8"/>
          <text x="728" y="245" text-anchor="middle" fill="#cdd6f4" font-size="9" font-family="system-ui">Prototype</text>

          <!-- ===== RIGHT BOTTOM: AI ===== -->
          <rect x="540" y="300" width="230" height="80" rx="12" fill="#2a2a3d" stroke="#3a3a4d" stroke-width="1"/>
          <text x="655" y="324" text-anchor="middle" fill="#9ca3af" font-size="11" font-weight="600" letter-spacing="1.5" font-family="system-ui">IA</text>

          <!-- Local AI -->
          <rect x="560" y="334" width="92" height="32" rx="5" fill="#2D7A5F" fill-opacity="0.12" stroke="#2D7A5F" stroke-width="0.8"/>
          <circle cx="576" cy="350" r="4" fill="#2D7A5F"/>
          <text x="612" y="354" text-anchor="middle" fill="#a6e3a1" font-size="9" font-weight="600" font-family="system-ui">IA locale (GPU)</text>

          <!-- Cloud AI -->
          <rect x="662" y="334" width="92" height="32" rx="5" fill="#89b4fa" fill-opacity="0.12" stroke="#89b4fa" stroke-width="0.8"/>
          <circle cx="678" cy="350" r="4" fill="#89b4fa"/>
          <text x="714" y="354" text-anchor="middle" fill="#89b4fa" font-size="9" font-weight="600" font-family="system-ui">API cloud (opt.)</text>

          <!-- Legend -->
          <line x1="50" y1="392" x2="70" y2="392" stroke="#2D7A5F" stroke-width="1.5" stroke-dasharray="6 3"/>
          <text x="78" y="396" fill="#9ca3af" font-size="9" font-family="system-ui">Reseau prive chiffre</text>

          <rect x="220" y="386" width="10" height="10" rx="2" fill="#2D7A5F" fill-opacity="0.3" stroke="#2D7A5F" stroke-width="0.8"/>
          <text x="238" y="396" fill="#9ca3af" font-size="9" font-family="system-ui">Heberge chez vous</text>

          <rect x="390" y="386" width="10" height="10" rx="2" fill="#89b4fa" fill-opacity="0.3" stroke="#89b4fa" stroke-width="0.8"/>
          <text x="408" y="396" fill="#9ca3af" font-size="9" font-family="system-ui">Cloud externe (optionnel)</text>
        </svg>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-6">

        <div class="card reveal reveal-d1 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#f0faf5;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="7" width="20" height="4" rx="1"/>
              <rect x="2" y="13" width="20" height="4" rx="1"/>
              <circle cx="6" cy="9" r="1" fill="#2D7A5F"/>
              <circle cx="6" cy="15" r="1" fill="#2D7A5F"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">Votre infrastructure</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Bare-metal, VM ou cloud prive. Aucune donnee ne transite par un tiers.
            Conformite RGPD par design.
          </p>
        </div>

        <div class="card reveal reveal-d2 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#fff8ed;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#D97706" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="2"/>
              <path d="M12 2v4"/><path d="M12 18v4"/>
              <path d="M4.93 4.93l2.83 2.83"/><path d="M16.24 16.24l2.83 2.83"/>
              <path d="M2 12h4"/><path d="M18 12h4"/>
              <path d="M4.93 19.07l2.83-2.83"/><path d="M16.24 7.76l2.83-2.83"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">Reseau prive Tailscale</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Chaque environnement rejoint votre mesh WireGuard automatiquement.
            Deployez des applis accessibles uniquement sur votre reseau prive.
          </p>
        </div>

        <div class="card reveal reveal-d3 p-8">
          <div class="w-12 h-12 flex items-center justify-center rounded-lg mb-5" style="background:#f0faf5;">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#2D7A5F" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <rect width="18" height="11" x="3" y="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
          </div>
          <h3 class="text-lg font-bold text-gray-900 mb-3" style="letter-spacing:-0.01em;">SSO et gestion centralisee</h3>
          <p class="text-gray-500 leading-relaxed" style="font-size:0.9375rem;">
            Authentification Entra ID / OIDC, gestion des cles SSH,
            secrets chiffres AES-256. Acces GPU natif pour les workloads IA.
          </p>
        </div>

      </div>
    </div>
  </section>

  <!-- ======================== FINAL CTA ======================== -->
  <section id="cta" class="py-20" style="background:#1e1e2e;">
    <div class="max-w-2xl mx-auto px-6 text-center">
      <div class="reveal">
        <h2 class="font-extrabold tracking-tight mb-4"
            style="font-size:clamp(1.75rem,3.5vw,2.75rem); letter-spacing:-0.02em; color:white;">
          Deployez votre cloud prive en 15 minutes
        </h2>
        <p class="mb-8" style="font-size:1.0625rem; line-height:1.65; color:#9ca3af;">
          Un serveur Linux. Une commande. Vos developpeurs codent aujourd'hui.
        </p>
        <div class="flex flex-wrap gap-3 justify-center mb-8">
          <a href="#" class="btn-primary" style="font-size:1rem; padding:0.875rem 2rem; background:#2D7A5F; border-color:#2D7A5F;">
            Deployer Plati
          </a>
          <a href="#" class="btn-outline" style="font-size:1rem; padding:0.875rem 2rem; color:white; border-color:rgba(255,255,255,0.3);">
            Voir la documentation
          </a>
        </div>

        <!-- Install snippet -->
        <div class="inline-flex items-center gap-3 rounded-lg px-5 py-3 font-mono text-sm" style="background:#2a2a3d; border:1px solid #3a3a4d; color:#cdd6f4; font-family:'JetBrains Mono',monospace;">
          <span style="color:#6b7280;">$</span>
          <span>curl -fsSL https://plati.dev/install | sh</span>
        </div>
      </div>
    </div>
  </section>

  <!-- ======================== FOOTER ======================== -->
  <footer class="py-8 bg-white" style="border-top:1px solid #E5E7EB;">
    <div class="max-w-6xl mx-auto px-6">
      <div class="flex flex-wrap justify-between items-center gap-4">

        <div class="flex items-center gap-2">
          <svg width="22" height="22" viewBox="0 0 30 30" fill="none">
            <rect width="30" height="30" rx="8" fill="#2D7A5F"/>
            <path d="M9 21C9 21 11 13 17 11C23 9 21 15 17 15C13 15 15 21 15 21"
              stroke="white" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <circle cx="21" cy="10" r="2.5" fill="#D97706"/>
          </svg>
          <span class="font-bold text-gray-900">plati</span>
        </div>

        <div class="flex gap-6">
          <a href="#" class="text-sm text-gray-400 no-underline hover:text-primary transition-colors">Documentation</a>
          <a href="#" class="text-sm text-gray-400 no-underline hover:text-primary transition-colors">GitHub</a>
          <a href="#" class="text-sm text-gray-400 no-underline hover:text-primary transition-colors">Contact</a>
        </div>

        <p class="text-sm text-gray-300 m-0">&copy; 2026 Plati. Open source.</p>
      </div>
    </div>
  </footer>

</div>
