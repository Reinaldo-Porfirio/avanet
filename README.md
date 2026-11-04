# AvaNet

Monitor de qualidade de internet para Windows. Fica em segundo plano medindo latência, estabilidade,
download e upload, e todo dia às 8h entrega um relatório do dia anterior: melhores e piores horários,
se o problema está no seu Wi-Fi ou na operadora e, com alguns dias de dados, os padrões da sua conexão.

> **Status:** em desenvolvimento. Fase 0 (viabilidade) e Fase 1 (fundação) concluídas.
> Planejamento completo em [docs/PLANEJAMENTO.md](docs/PLANEJAMENTO.md).

## Princípios

- **Seus dados ficam no seu computador.** Sem conta, sem servidor, sem telemetria.
- **Não gasta sua franquia.** Medições leves o tempo todo; testes de velocidade com orçamento diário.
  Em rede móvel, só se você ativar.
- **Não precisa de administrador.**

## Desenvolvimento

Requisitos: [Go](https://go.dev/dl/) (versão do `go.mod`) e Git.

```powershell
go run ./cmd/avanet            # inicia o agente (Ctrl+C para sair)
go run ./cmd/avanet version
go test ./...
.\scripts\check.ps1            # todas as verificações do CI (rode antes de cada commit)
```

Para usar uma pasta de dados separada durante o desenvolvimento:

```powershell
$env:AVANET_DATA_DIR = "$env:TEMP\avanet-dev"
```

### Estrutura

| Pasta | Conteúdo |
|---|---|
| `cmd/avanet` | ponto de entrada; só monta o contexto e chama `internal/app` |
| `internal/app` | linha de comando e ligação entre os pacotes |
| `internal/config` | configuração do processo, validada na inicialização |
| `internal/logging` | logs JSON com rotação por tamanho |
| `internal/storage` | SQLite: abertura segura, verificação de integridade, migrações |
| `spikes/` | experimentos da Fase 0 (descartáveis, guardados como registro de decisão) |

## Licença

Ainda sem licença definida: o código está visível para consulta, mas todos os direitos são
reservados ao autor. A escolha da licença está nas perguntas em aberto do planejamento.
