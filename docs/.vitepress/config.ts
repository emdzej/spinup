import { defineConfig } from 'vitepress';
import { withMermaid } from 'vitepress-plugin-mermaid';

export default withMermaid(
  defineConfig({
    title: 'SpinUP',
    description: 'Cloud-functions-style platform for Spin apps on Kubernetes',
    cleanUrls: true,
    lastUpdated: true,

    // Local dev + curl examples reference localhost. Don't fail the build
    // when VitePress tries to resolve them as internal links.
    ignoreDeadLinks: [
      /^http:\/\/localhost/,
      /^https?:\/\/spinup\.example\.com/
    ],

    markdown: {
      // shiki: promql doesn't have a bundled grammar; alias to a similar one
      // to silence the warning.
      languageAlias: { promql: 'python' }
    },

    themeConfig: {
      nav: [
        { text: 'Guide', link: '/guide/introduction' },
        { text: 'Install', link: '/install/requirements' },
        { text: 'User Guide', link: '/user-guide/creating-applications' },
        { text: 'Architecture', link: '/architecture/overview' },
        { text: 'Reference', link: '/reference/http-api' },
        { text: 'License', link: '/license' }
      ],

      sidebar: {
        '/guide/': [
          {
            text: 'Getting Started',
            items: [
              { text: 'Introduction', link: '/guide/introduction' },
              { text: 'Quick start', link: '/guide/quick-start' },
              { text: 'Core concepts', link: '/guide/concepts' }
            ]
          }
        ],
        '/install/': [
          {
            text: 'Installation',
            items: [
              { text: 'Requirements', link: '/install/requirements' },
              { text: 'Local development', link: '/install/local-dev' },
              { text: 'Production (Helm)', link: '/install/production' }
            ]
          }
        ],
        '/user-guide/': [
          {
            text: 'User Guide',
            items: [
              { text: 'Creating applications', link: '/user-guide/creating-applications' },
              { text: 'Writing functions', link: '/user-guide/writing-functions' },
              { text: 'Building & deploying', link: '/user-guide/building-and-deploying' },
              { text: 'Invoking functions', link: '/user-guide/invoking' },
              { text: 'Logs & metrics', link: '/user-guide/logs-and-metrics' }
            ]
          }
        ],
        '/architecture/': [
          {
            text: 'Architecture',
            items: [
              { text: 'Overview', link: '/architecture/overview' },
              { text: 'Control plane', link: '/architecture/control-plane' },
              { text: 'Builders', link: '/architecture/builders' },
              { text: 'Observability', link: '/architecture/observability' }
            ]
          },
          {
            text: 'Roadmap',
            items: [
              { text: 'Scaling roadmap', link: '/architecture/scaling-roadmap' }
            ]
          }
        ],
        '/reference/': [
          {
            text: 'Reference',
            items: [
              { text: 'HTTP API', link: '/reference/http-api' },
              { text: 'Control-plane env', link: '/reference/configuration' },
              { text: 'Helm chart values', link: '/reference/chart-values' },
              { text: 'Database schema', link: '/reference/database' }
            ]
          }
        ]
      },

      socialLinks: [
        { icon: 'github', link: 'https://github.com/emdzej/spinup' }
      ],

      search: { provider: 'local' },

      footer: {
        message: 'Alpha — API and chart values may change.',
        copyright: 'SpinUP contributors'
      }
    },

    mermaid: {
      theme: 'default'
    }
  })
);
