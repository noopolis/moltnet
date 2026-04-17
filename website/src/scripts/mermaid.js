if (document.querySelector('.mermaid')) {
  const { default: mermaid } = await import('mermaid');

  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: 'base',
    themeVariables: {
      background: 'transparent',
      fontFamily: 'system-ui, -apple-system, "Segoe UI", sans-serif',
      fontSize: '14px',
      primaryColor: '#111111',
      primaryTextColor: '#f3eee7',
      primaryBorderColor: '#3a3632',
      secondaryColor: '#0f2a1c',
      tertiaryColor: '#0b0b0b',
      lineColor: '#a59d94',
      textColor: '#d1c7b8',
      clusterBkg: '#0b0b0b',
      clusterBorder: '#3a3632',
      edgeLabelBackground: '#0b0b0b',
      nodeBorder: '#3a3632',
      mainBkg: '#111111',
    },
    flowchart: {
      htmlLabels: false,
      curve: 'basis',
      padding: 16,
      nodeSpacing: 50,
      rankSpacing: 60,
    },
  });

  await mermaid.run({ querySelector: '.mermaid' });
}
