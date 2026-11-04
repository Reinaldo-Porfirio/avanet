# AvaNet — instruções para o assistente

- `docs/PLANEJAMENTO.md` é a fonte da verdade. Decisões novas ou alteradas entram lá.
- O dono está **aprendendo Go** neste projeto: ao escrever código, explicar em português os
  conceitos de Go que aparecem pela primeira vez (sem repetir os já explicados).
- Padrões obrigatórios: código limpo, sem gambiarras, erros sempre tratados ou propagados com
  contexto, segurança em primeiro lugar (seção 10 do planejamento). Se um atalho for tentador,
  fazer direito ou registrar o compromisso.
- Código, identificadores, logs e mensagens de erro internas em **inglês**; textos da interface
  e saída da linha de comando em **português**.
- Migrações em `internal/storage/migrations` são **somente acréscimo**: nunca editar uma que já
  existe (o app recusa iniciar se o checksum mudar). Corrigir com uma nova migração.
- Antes de dar uma tarefa por concluída: `.\scripts\check.ps1` precisa passar.
- O Go foi instalado recentemente; se `go` não estiver no PATH do terminal, usar
  `C:\Program Files\Go\bin`.
