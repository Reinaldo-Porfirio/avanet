# Fase 0 — experimentos de viabilidade

Código descartável, guardado como registro das decisões. É um módulo Go separado para que as
dependências testadas aqui não entrem no projeto principal.

**Não é código de produção:** passa em `go vet` e `gofmt`, mas não segue o padrão do linter
(ex.: erros de `Close` ignorados, sem testes). Nada daqui é copiado para `internal/`; as fases
seguintes reimplementam cada parte com testes, tratamento de erro completo e o lint do projeto.

**Executado em 2026-11-04** no PC do dono: Windows 11 Pro 23H2 (build 22631), usuário **sem**
privilégio de administrador, Wi-Fi 6E (5 GHz, sinal 97%), internet de ~600 Mbps / ~40 Mbps.

```powershell
go run ./cmd/adapters    # (b) gateway e tipo de conexão, (e) conexão limitada
go run ./cmd/latency     # (a) latência: 3 métodos × 3 alvos
go run ./cmd/wifi        # (d) Wi-Fi: API nativa × netsh
go run ./cmd/speedtest   # (c) velocidade via Cloudflare (gasta ~400 MB!)
go run ./cmd/desktop     # (f) ícone na bandeja + notificação (20 s)
```

## Resultados

### (a) Latência sem administrador

| Alvo | ICMP — API do Windows | ICMP — pro-bing | TCP connect |
|---|---|---|---|
| Roteador | 0 ms (resolução de 1 ms) | **0,59 ms** | 0,95 ms (porta 80) |
| 1.1.1.1 | 11 ms | **12,1 ms** | 14,7 ms |
| 8.8.8.8 | 59 ms | **58,9 ms** | 63,0 ms |

- **pro-bing funciona sem administrador** no Windows e mede em microssegundos → **escolhido**.
- A API `IcmpSendEcho` também funciona sem admin, mas só tem resolução de 1 ms: inútil para o
  roteador (sempre "0"). Fica como plano B se o pro-bing falhar em alguma máquina.
- TCP connect soma o custo do handshake (+1–5 ms) e depende de uma porta aberta no roteador.
- **Os alvos importam muito:** daqui o 8.8.8.8 está a 59 ms e o 1.1.1.1 a 11 ms. Usar vários
  alvos e considerar o **menor** valor como "latência da internet" evita culpar a operadora pela
  rota de um serviço específico.

### (b) Gateway e (e) tipo de conexão / conexão limitada

- `GetAdaptersAddresses` (x/sys/windows) retorna adaptador ativo, tipo (Wi-Fi/cabo/modem móvel),
  gateway e velocidade do link — sem admin.
- `INetworkCostManager::GetCost` (COM) retorna o custo da conexão: aqui `0x1` = ilimitada. É o
  mesmo dado que o Windows usa para "conexão limitada", marcada automaticamente em hotspots de
  celular → **base da detecção de rede móvel**. Validar com um hotspot real na Fase 3.

### (c) Velocidade — Cloudflare

| Transferência | Download | Upload |
|---|---|---|
| 0,1 MB | 64 Mbps | 33 Mbps |
| 1 MB | 136 Mbps | 40 Mbps |
| 10 MB | 391 Mbps | 35 Mbps (2,3 s → parou) |
| 25 MB | 564 Mbps | — |
| 50 MB | 657 Mbps (0,6 s) | — |
| **Gasto** | **86 MB** | **11 MB** |

- Funciona com cliente HTTP próprio (`/__down` e `/__up`), sem biblioteca.
- **Download em link rápido ainda estava subindo a 50 MB** (a conexão TCP leva tempo para
  "acelerar"). Medir a vazão só na parte final da transferência, descartando a aceleração
  inicial, deve corrigir isso sem gastar mais dados → implementar na Fase 3.
- Upload estabiliza cedo: 10 MB bastam.
- Um teste completo gastou **~100 MB** nesta conexão → com 2 GB/dia cabem ~20 testes.
- A referência com 4 conexões paralelas mediu menos (463 Mbps), porque o cronômetro incluiu a
  abertura das conexões. O teste sequencial é suficiente.

### (d) Wi-Fi

- API nativa (`wlanapi.dll`) leu SSID, sinal (%), RSSI (dBm), canal, tipo de rádio e taxa do
  link, sem admin → **escolhida**.
- `netsh` respondeu com rótulos em português ("Sinal", "Canal", "Faixa") → confirmado que
  depende do idioma do Windows. Descartado.
- ⚠️ A partir do **Windows 11 24H2**, ler o SSID pela API pode exigir que o usuário permita
  "acesso à localização" para apps de desktop. Não reproduzível nesta máquina (23H2). O app deve
  funcionar sem o SSID (sinal, canal e taxa continuam) e explicar a permissão na interface.

### (f) Bandeja e notificação

- `fyne.io/systray`: ícone e menu funcionaram sem admin.
- `go-toast/v2` (API WinRT via COM, sem PowerShell): notificação enviada sem erro. Registra o app
  em `HKCU\SOFTWARE\Classes\AppUserModelId\<AppID>` (sem admin). A chave do teste foi removida.
- Pendente: confirmação visual do dono de que a notificação apareceu.

## Servidor de teste: termos e privacidade

| | Cloudflare | M-Lab (ndt7) | Ookla |
|---|---|---|---|
| Uso por terceiros | Endpoints públicos sem termos específicos publicados; a biblioteca oficial da Cloudflare (`cloudflare/speedtest`, MIT) usa os mesmos | Permitido | Proibido uso comercial |
| Privacidade | Recebe o IP (inevitável em qualquer teste); nada é publicado | **Publica o IP e o horário de cada teste em base pública, para sempre** | — |
| Decisão | ✅ **Escolhido** | ❌ Descartado (fere o princípio de privacidade). **Não foi executado**, para não publicar o IP do dono. | ❌ |

Risco aceito: a Cloudflare pode mudar ou limitar os endpoints. O cliente de velocidade fica atrás
de uma interface, para trocar de servidor sem mexer no resto.
