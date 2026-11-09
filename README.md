# AvaNet

Monitor de qualidade de internet para Windows. Fica em segundo plano medindo latência, estabilidade,
download e upload, e todo dia às 8h entrega um relatório do dia anterior: melhores e piores horários,
se o problema está no seu Wi-Fi ou na operadora e, com alguns dias de dados, os padrões da sua conexão.

> **Status:** em desenvolvimento. Fases 0 a 5 concluídas: monitoramento em segundo plano e boletim diário no terminal.
> Planejamento completo em [docs/PLANEJAMENTO.md](docs/PLANEJAMENTO.md).

## Princípios

- **Seus dados ficam no seu computador.** Sem conta, sem servidor, sem telemetria.
- **Não gasta sua franquia.** Medições leves o tempo todo; testes de velocidade com orçamento diário.
  Em rede móvel, só se você ativar.
- **Não precisa de administrador.**

## Desenvolvimento

Requisitos: [Go](https://go.dev/dl/) (versão do `go.mod`) e Git.

```powershell
go run ./cmd/avanet run        # monitora e escreve uma linha por medição (Ctrl+C para sair)
go run ./cmd/avanet status     # resumo de hoje
go run ./cmd/avanet report     # boletim de ontem ("hoje" ou AAAA-MM-DD também valem)
go run ./cmd/avanet probe      # diagnóstico rápido: conexão, Wi-Fi e latência agora
go run ./cmd/avanet speedtest  # download e upload (respeita o orçamento diário de dados)
go run ./cmd/avanet profile    # perfis de uso; "profile jogos" escolhe um
go run ./cmd/avanet version
go test ./...
.\scripts\check.ps1            # todas as verificações do CI (rode antes de cada commit)
```

### Instalar no seu PC (bandeja + iniciar com o Windows)

```powershell
$dest = "$env:LOCALAPPDATA\Programs\AvaNet"
go build -trimpath -o "$dest\avanet.exe" ./cmd/avanet
go build -trimpath -ldflags "-H=windowsgui" -o "$dest\avanetw.exe" ./cmd/avanetw
& "$dest\avanet.exe" autostart on   # inicia com o Windows (desligue com: autostart off)
Start-Process "$dest\avanetw.exe"   # inicia agora, com o ícone perto do relógio
```

`avanetw.exe` é a versão sem janela (como `pythonw.exe`): só o ícone na bandeja.

Para usar uma pasta de dados separada durante o desenvolvimento:

```powershell
$env:AVANET_DATA_DIR = "$env:TEMP\avanet-dev"
```

### Estrutura

| Pasta | Conteúdo |
|---|---|
| `cmd/avanet` | ponto de entrada; só monta o contexto e chama `internal/app` |
| `cmd/avanetw` | a mesma coisa sem janela de console (bandeja) |
| `internal/app` | linha de comando e ligação entre os pacotes |
| `internal/agent` | monitor em segundo plano: rodadas, agenda dos testes, quedas, sessões |
| `internal/analysis`, `internal/report` | análise do dia e boletim das 8h |
| `internal/notify` | notificação do Windows |
| `internal/tray` | ícone na bandeja, colorido pela qualidade da conexão |
| `internal/autostart`, `internal/instance` | iniciar com o Windows; uma instância por vez |
| `internal/ptbr` | números no formato brasileiro |
| `internal/config` | configuração do processo, validada na inicialização |
| `internal/domain` | tipos e regras puras: estatísticas de latência, rede, Wi-Fi, quedas |
| `internal/netinfo` | interface usada para a internet, roteador, conexão limitada |
| `internal/wifi` | estado do Wi-Fi pela API nativa do Windows |
| `internal/probe` | sondas de medição: latência (ICMP) e velocidade (Cloudflare) |
| `internal/logging` | logs JSON com rotação por tamanho |
| `internal/storage` | SQLite: abertura segura, verificação de integridade, migrações |
| `spikes/` | experimentos da Fase 0 (descartáveis, guardados como registro de decisão) |

## Licença

Ainda sem licença definida: o código está visível para consulta, mas todos os direitos são
reservados ao autor. A escolha da licença está nas perguntas em aberto do planejamento.
