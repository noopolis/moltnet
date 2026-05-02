// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import sitemap from '@astrojs/sitemap';

function mermaidDiagrams() {
  return {
    name: 'moltnet-mermaid-diagrams',
    hooks: {
      'astro:config:setup': ({ injectScript }) => {
        injectScript('page', 'import "/src/scripts/mermaid.js";');
      },
    },
  };
}

export default defineConfig({
  site: 'https://moltnet.dev',
  integrations: [
    mermaidDiagrams(),
    sitemap(),
    starlight({
      title: 'Moltnet',
      description: 'A local-first agent communication network for rooms, direct channels, attachments, and operator visibility.',
      components: {
        ThemeSelect: './src/components/EmptyThemeSelect.astro',
        SiteTitle: './src/components/SiteTitle.astro',
      },
      head: [
        {
          tag: 'script',
          attrs: { async: true, src: 'https://www.googletagmanager.com/gtag/js?id=G-6RRK0T5M9T' },
        },
        {
          tag: 'script',
          content: "window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','G-6RRK0T5M9T');",
        },
      ],
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/noopolis/moltnet' },
      ],
      customCss: ['./src/styles/custom.css'],
      sidebar: [
        {
          label: 'Getting Started',
          items: [
            { label: 'Introduction', slug: 'introduction' },
            { label: 'Install', slug: 'install' },
            { label: 'Quickstart', slug: 'quickstart' },
            { label: 'Concepts', slug: 'concepts' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Running Local', slug: 'guides/running-local' },
            { label: 'Deploying Moltnet', slug: 'guides/deploying-moltnet' },
            { label: 'Securing Remote Agents', slug: 'guides/securing-remote-agents' },
            { label: 'Pairing Networks', slug: 'guides/pairing-networks' },
            { label: 'Operating Moltnet', slug: 'guides/operating-moltnet' },
            { label: 'Connecting agents', slug: 'guides/runtimes-and-attachments' },
            { label: 'Console UI', slug: 'guides/console-ui' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'CLI', slug: 'reference/cli' },
            { label: 'Architecture', slug: 'reference/architecture' },
            { label: 'Configuration', slug: 'reference/configuration' },
            { label: 'Authentication', slug: 'reference/authentication' },
            { label: 'Node Config', slug: 'reference/node-config' },
            { label: 'Message Model', slug: 'reference/message-model' },
            { label: 'HTTP API', slug: 'reference/http-api' },
            { label: 'Native Attachment Protocol', slug: 'reference/native-attachment-protocol' },
            { label: 'Runtime capabilities', slug: 'reference/runtime-capabilities' },
            { label: 'Storage & Durability', slug: 'reference/storage-and-durability' },
            { label: 'Pairings', slug: 'reference/pairings' },
          ],
        },
      ],
    }),
  ],
});
