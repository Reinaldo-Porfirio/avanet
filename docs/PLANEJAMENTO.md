# AvaNet — Planejamento

> **Status:** Fases 0 a 4 concluídas. Próxima: Fase 5 (relatório diário).
> **Criado em:** 2026-11-04 · **Última revisão:** 2026-11-04
> Este documento é a fonte da verdade do projeto. Decisões novas entram aqui.

---

## 1. Visão

App de **Windows** que fica em **segundo plano** medindo a qualidade da internet do próprio
computador. Todo dia às **8h** ele avisa por notificação que o **relatório de ontem** está pronto:
gráfico de download, upload e latência ao longo do dia, maior e menor valor (com horário),
faixas de horário boas e ruins e quanto do dia foi de fato monitorado. Com **3 dias ou mais**
de dados, aponta **padrões** (ex.: "a internet piora todo dia entre 20h e 23h").

Diferenciais em relação a um speedtest comum:
- **Diagnóstico Wi-Fi × operadora:** mede a latência até o roteador **e** até a internet. Assim
  dá para dizer se o problema está na casa (Wi-Fi) ou na operadora.
- **Perfis de uso** com explicação simples, para quem não entende de rede.
- **Comparação com o plano contratado** (referência: regras da Anatel), útil para reclamar com
  a operadora.

## 2. Decisões tomadas

| Tema | Decisão |
|---|---|
| Nome | **AvaNet** (de "avaliação de rede"). |
| Objetivo | **Portfólio** (infra/cloud) + uso pessoal. Produto é um "talvez": a arquitetura e as licenças escolhidas não podem impedir isso. |
| Plataforma | **Somente Windows** (10/11) no MVP. Android depois; iOS fora (não permite tarefa periódica confiável em segundo plano). |
| Linguagem | **Go**: executável único, leve em segundo plano, muito usado em ferramentas de infra. O dono está aprendendo Go durante o projeto (seção 11). |
| Interface | Ícone na **bandeja do sistema** + painel **web local** aberto no navegador (mesmo modelo do Syncthing). Detalhes na seção 8. |
| Dados | **100% locais** (SQLite). Sem conta, sem servidor, sem telemetria. |
| Custo | **Zero**. |
| Relatório | **Notificação diária às 8h** com o resumo de ontem; relatório completo na **aba Relatórios**. Se o PC estiver desligado às 8h, gera e avisa ao ligar. |
| Perfis | Perfis prontos com descrição simples + **perfil Completo** (todas as métricas). Seção 5. |
| Dados móveis | **Desligado por padrão.** Se o usuário ativar, roda com teto diário baixo. Seção 6. |
| Vários dispositivos | **Fora do escopo.** Fica no backlog de baixa probabilidade (seção 13). |
| Servidor de velocidade | **Cloudflare** (`speed.cloudflare.com`). M-Lab descartado por publicar o IP do usuário; Ookla por licença (Fase 0). |
| Design | **Boletim diário**: linguagem de jornal/previsão do tempo + relógio de 24h. Não genérico, mas legível para leigos (seção 7). |
| Qualidade | Código limpo, sem gambiarras, boas práticas de segurança (seção 10). |

## 3. Análise de viabilidade

**Veredito: viável**, com estas limitações assumidas no desenho:

1. **Teste de velocidade consome muitos dados.** Por isso existem dois tipos de medição:
   - **Leve** (latência, jitter, perda de pacotes, sinal do Wi-Fi): poucos KB, roda a cada 1–5 min.
   - **Pesada** (download/upload): dezenas de MB, roda poucas vezes por hora, sempre dentro de
     um **orçamento diário de dados**.
2. **O PC não fica ligado o dia todo.** O relatório mostra a **cobertura** (ex.: "monitorado
   14h de 24h") e o gráfico deixa os buracos visíveis. Nenhum dado é inventado ou interpolado.
3. **O uso do próprio usuário interfere na medição.** Um download pesado rodando ao mesmo tempo
   derruba o resultado do teste. Mitigação: registrar o tráfego da placa de rede no momento do
   teste e marcar a medição como "possivelmente afetada por uso local".
4. **Mínimo e máximo enganam** (um pico isolado distorce tudo). O relatório mostra mín/máx com
   horário, mas as conclusões usam **mediana e percentis** (p10/p90).
5. **Três dias é pouco para afirmar padrão.** Os insights são chamados de "padrão provável", com
   nível de confiança que cresce com o número de dias e de amostras.
6. **Links muito rápidos (> ~300 Mbps)** podem ter a velocidade **subestimada** por causa do teto
   de bytes por teste. Na Fase 0, a 50 MB a medição ainda subia num link de ~600 Mbps. Mitigação
   na Fase 3: medir a vazão só depois da aceleração inicial da conexão.

### Resultado da Fase 0 (2026-11-04, PC do dono, usuário sem administrador)

Detalhes, números e código em [`spikes/phase0`](../spikes/phase0/README.md).

| Pergunta | Resposta | Decisão |
|---|---|---|
| (a) Latência sem admin? | ✅ ICMP via **pro-bing** funciona e mede em µs (roteador 0,59 ms). A API `IcmpSendEcho` só tem resolução de 1 ms. | pro-bing; `IcmpSendEcho` como plano B |
| (b) Achar o gateway? | ✅ `GetAdaptersAddresses`: adaptador, tipo, gateway e velocidade do link | adotado |
| (c) Velocidade? | ✅ Cloudflare com cliente HTTP próprio: ~657 / 40 Mbps, **~100 MB por teste** | Cloudflare; M-Lab descartado (privacidade), Ookla descartado (licença) |
| (d) Ler o Wi-Fi? | ✅ `wlanapi.dll`: sinal, RSSI, canal, rádio, taxa. `netsh` responde em português → descartado | API nativa |
| (e) Rede limitada/móvel? | ✅ `INetworkCostManager` (COM) retorna o custo da conexão, o mesmo usado para hotspots | adotado; validar com hotspot real na Fase 3 |
| (f) Bandeja e notificação? | ✅ `fyne.io/systray` + `go-toast/v2` (WinRT, sem PowerShell), sem admin | adotados; falta confirmação visual |

Outros achados:
- **A escolha dos alvos de latência importa:** 1.1.1.1 a 11 ms e 8.8.8.8 a 59 ms daqui. A
  "latência da internet" passa a ser o **menor** valor entre vários alvos, para não culpar a
  operadora pela rota de um serviço específico.
- Orçamento: com ~100 MB por teste, 2 GB/dia comportam ~20 testes. Os intervalos dos perfis
  (seção 5) são recalibrados na Fase 3.

### Resultado da Fase 3 (2026-11-05, mesma máquina)

| Achado | Consequência |
|---|---|
| A latência pelo "tempo até o primeiro byte" HTTP deu **51 ms** contra 11–15 ms do ping: o servidor da Cloudflare gasta ~40 ms processando cada requisição (`Server-Timing: cfSpeedWorker`). | Latência do teste = tempo do **handshake TCP** (puro tempo de rede): 15–17 ms. |
| O upload é cronometrado pelo **relógio do próprio servidor** (`cfSpeedWorker`), como faz o medidor oficial da Cloudflare. Validação: relógio do cliente e do servidor concordaram com < 1% de diferença (102,0 × 102,4 Mbps). | Relógio do cliente fica como alternativa se o cabeçalho sumir. |
| O `curl` do Windows mediu ~25% a menos de upload, mas o relógio do servidor mostrou o próprio `curl` enviando mais devagar. | `curl` no Windows **não serve de referência** de upload. |
| Descartar os primeiros 30% de cada transferência (aceleração do TCP) elevou o download de 657 para **660–730 Mbps**. | Correção adotada. |
| As respostas da Cloudflare trazem **IP, cidade e coordenadas do usuário** em cabeçalhos `cf-meta-*`. | O cliente nunca lê esses cabeçalhos; o servidor é identificado pelo `CF-RAY` (datacenter). |
| Um teste completo num link de ~700/100 Mbps usa **~130 MB** (90 MB down + 40 MB up). | 2 GB/dia comportam ~15 testes. Perfis com teste de hora em hora precisam de ajuste no agendador (Fase 4). |

### Riscos

1. **Endpoints da Cloudflare não têm termos publicados para uso por terceiros** (são os mesmos
   que a biblioteca oficial `cloudflare/speedtest`, licença MIT, usa). A Cloudflare pode mudá-los
   ou limitá-los. Mitigação: o teste de velocidade fica atrás de uma interface, para trocar de
   servidor sem mexer no resto. Antes de virar produto, pedir confirmação à Cloudflare.
2. **Executável sem assinatura digital.** O Windows SmartScreen mostra um aviso ao executar, e
   alguns antivírus dão falso positivo em binários Go. Aceitável para portfólio; assinar o
   código (que é pago) só faria sentido se virar produto.
3. **Windows 11 24H2+ pode exigir permissão de localização para ler o nome da rede Wi-Fi.** Não
   reproduzível na máquina do dono (23H2). O app funciona sem o SSID (sinal, canal e taxa
   continuam) e explica a permissão na interface.

## 4. Métricas coletadas

| Métrica | Como | Custo de dados |
|---|---|---|
| Latência até o **roteador** (gateway) | ICMP (pro-bing) para o gateway padrão | ~0 |
| Latência até a **internet** | ICMP (pro-bing) para vários alvos fixos; vale o **menor** valor da rodada | ~0 |
| Jitter | variação entre as latências de uma rodada | ~0 |
| Perda de pacotes | % de pacotes sem resposta na rodada | ~0 |
| Quedas de conexão | sequência de rodadas sem resposta → evento "offline" com início e fim | ~0 |
| Wi-Fi | SSID, sinal (%/dBm), banda (2,4/5/6 GHz), canal, taxa do link | 0 (lido do sistema) |
| Tipo de conexão | cabo / Wi-Fi / móvel (hotspot/modem), conexão limitada sim/não | 0 |
| Download / upload | transferência HTTPS com tamanho crescente e teto de bytes | alto |
| Tráfego local | bytes da placa de rede durante o teste (detecta interferência) | 0 |

**Diagnóstico Wi-Fi × operadora** (regra inicial, a calibrar):
- gateway ruim → "problema provavelmente no Wi-Fi/roteador";
- gateway bom, internet ruim → "problema provavelmente na operadora";
- os dois bons, mas velocidade baixa → "possível limitação do plano ou congestionamento".

## 5. Perfis de uso

O perfil define **quais métricas pesam mais** no relatório, **o que conta como bom/ruim** e
**com que frequência medir**. O usuário escolhe na primeira execução e pode trocar depois. A
descrição abaixo é o texto que aparece na interface.

| Perfil | Descrição para o usuário | Foco | Leve | Velocidade |
|---|---|---|---|---|
| **Uso básico** | "Navegar, redes sociais, WhatsApp e e-mail. Mede pouco e economiza recursos." | estabilidade, quedas | 5 min | a cada 3 h |
| **Streaming** | "Assistir Netflix, YouTube e afins. Foca em velocidade de download estável para o vídeo não travar." | download | 5 min | a cada 1h30 |
| **Jogos online** | "Para quem joga online. Foca no tempo de resposta (ping) e em travadinhas (jitter e perda), que importam mais que velocidade." | latência, jitter, perda | 1 min | a cada 3 h |
| **Home office** | "Videochamadas e trabalho remoto. Foca em upload e estabilidade, que é o que faz a chamada travar ou cortar." | upload, jitter, perda | 2 min | a cada 1h30 |
| **Completo** | "Mede tudo, o tempo todo, dentro do limite de dados. Para quem quer todos os números." | todas | 1 min | a cada 1 h* |

\* Sempre limitado pelo orçamento diário de dados (seção 6): com ~130 MB por teste num link rápido,
2 GB/dia comportam ~15 testes, e o agendador espaça os testes quando o intervalo do perfil não
caberia (Fase 4). Intervalos de velocidade recalibrados na Fase 4 a partir dos números da Fase 3.

**Limiares iniciais** (valores de partida, ajustados com dados reais):

| Perfil | Bom | Ruim |
|---|---|---|
| Jogos online | latência < 50 ms, jitter < 15 ms, perda < 1% | latência > 100 ms ou perda > 2% |
| Home office | upload ≥ 3 Mbps, jitter < 30 ms, perda < 1% | upload < 1,5 Mbps ou perda > 3% |
| Streaming | download ≥ 25 Mbps (4K) / ≥ 5 Mbps (HD) | download < 5 Mbps |
| Uso básico | download ≥ 10 Mbps, sem quedas | quedas recorrentes |

Todos os perfis coletam as mesmas métricas leves; o perfil muda frequência, pesos e textos.
Os dados brutos são sempre os mesmos, então trocar de perfil recalcula relatórios antigos.

## 6. Orçamento de dados e redes móveis

No Windows, "dados móveis" significa **hotspot do celular** ou **modem USB/4G**.

| Situação | Padrão |
|---|---|
| Cabo / Wi-Fi comum | Teto de **2 GB/dia**. Cada teste: até **100 MB de download e 45 MB de upload** (permite chegar ao degrau de 50 MB). ✅ Fase 3 |
| Rede móvel | **Desligado.** Se ativado: teto de **50 MB/dia** (configurável até 200 MB); cada teste até **6 MB down / 2 MB up**. Métricas leves seguem normais. ✅ Fase 3 |

O consumo fica gravado no banco (`data_usage`, por dia local e tipo de rede), então o teto
vale mesmo reiniciando o app. Um teste só começa se o saldo do dia cobrir o **pior caso** dele,
de modo que o teto nunca é ultrapassado; bytes de um teste interrompido também são cobrados.
Por enquanto o teste em rede móvel exige `avanet speedtest -movel` a cada vez; a configuração
permanente vem com o painel (Fase 6).

- **Detecção de rede móvel:** usar a marcação de "conexão limitada" do Windows
  (`INetworkCostManager`, validado na Fase 0) e o tipo do adaptador. O usuário também pode
  marcar uma rede manualmente como móvel.
- **Gatilho adaptativo:** se a latência piorar de repente, roda um teste de velocidade extra para
  confirmar (no máximo 1 extra por hora, dentro do orçamento).
- **Teste com tamanho crescente:** degraus de 0,1 → 1 → 4 → 10 → 25 → 50 MB; para quando uma
  transferência dura ≥ 1 s ou o próximo degrau não cabe no teto. Uma internet lenta não gasta
  dados à toa e uma rápida ainda é bem medida.
- **Uso local:** os contadores da placa de rede antes e depois do teste mostram se outros
  programas usaram a rede ao mesmo tempo (excesso > 15% do teste e > 1 MB); o resultado sai
  marcado como "pode estar abaixo do real".
- O consumo do próprio app aparece na interface ("hoje: 340 MB de 2 GB").

## 7. Relatórios

### Relatório diário (gerado às 8h, cobrindo ontem 00:00–23:59)

- Gráfico do dia: download, upload e latência, com os **períodos sem monitoramento** visíveis.
- **Cobertura:** horas monitoradas, nº de medições.
- Mínimo e máximo **com horário**; mediana, p10 e p90.
- **Melhores e piores faixas de horário.**
- Quedas de conexão (quantas, quando, duração).
- Diagnóstico Wi-Fi × operadora.
- **Nota do dia segundo o perfil** (bom / regular / ruim), em linguagem simples.
- Consumo de dados do próprio app.

Se o PC estiver desligado às 8h, o relatório é gerado **ao ligar** e a notificação sai na hora.
Dias sem nenhum dado não geram relatório. A aba mostra "sem dados neste dia".

### Padrões (com 3 dias ou mais)

- Agrupa as medições por **faixa de horário** e compara entre os dias.
- Aponta faixas que ficaram ruins em **todos** os dias com cobertura suficiente.
- Exemplo de texto: *"Nos últimos 3 dias, sua velocidade caiu em média 60% entre 20h e 23h.
  Isso costuma indicar congestionamento na operadora no horário de pico. (Confiança: média)"*
- A confiança sobe com mais dias e mais amostras por faixa. Sem cobertura mínima, o padrão não
  é mostrado.

### Comparação com o plano contratado (Fase 7)

O usuário informa a velocidade contratada e o relatório compara com a referência da Anatel.
O texto deixa claro que é uma **medição indicativa**: medir pelo Wi-Fi inclui as limitações do
Wi-Fi, e o ideal para esse fim é o cabo.

**Regra ainda não confirmada** (pesquisa da Fase 0): os 80% (média mensal) e 40% (instantânea)
vêm do regulamento de 2011, revogado pelo **RQUAL (Resolução nº 717/2019)**. Reportagens citam
que o RQUAL mantém a média de 80% para as grandes operadoras, mas o percentual instantâneo
atual não foi encontrado em fonte oficial. Ler o texto da resolução antes da Fase 7.

### Direção visual do painel: "o boletim da sua internet"

Conceito navegável (dados fictícios): [`docs/design/conceito-relatorio.html`](design/conceito-relatorio.html).

A ideia é fugir do "painel de admin genérico" (cards cinza, gráficos de linha e velocímetro)
sem afastar quem não entende de rede:

- **Linguagem de jornal + previsão do tempo.** O relatório é um *boletim*: manchete em uma
  frase ("Dia firme, noite instável."), períodos do dia descritos como clima ("Céu limpo",
  "Tempestade") e padrões como *previsão*. Todo mundo sabe ler uma previsão do tempo.
- **Elemento-assinatura: o relógio de 24h.** Um anel com 24 fatias coloridas pela qualidade de
  cada hora; horas sem medição aparecem hachuradas (nada é inventado). O mesmo código de cores
  se repete como faixas atrás do gráfico de velocidade.
- **Tipografia editorial:** serifada expressiva nos títulos (Fraunces), sem serifa nos textos
  (IBM Plex Sans) e monoespaçada nos números (IBM Plex Mono). As fontes (licença OFL) vão
  **embutidas no executável**: o painel não carrega nada da internet.
- **Cores com significado, não decoração:** papel quente + tinta escura; qualidade em verde-
  -azulado (boa), ocre (regular) e vermelhão (ruim). Tema escuro "noite". Os dois temas seguem a
  preferência do Windows e podem ser trocados.
- **Acessível:** o significado nunca depende só da cor (rótulos, hachuras, linha contínua ×
  tracejada), contraste adequado, foco visível, layout funcional em telas estreitas.
- **Texto antes de número:** cada seção abre com a conclusão em português simples; os números
  detalhados ficam abaixo, para quem quer.

## 8. Arquitetura

### Visão geral

```
 ┌───────────────────────── avanet.exe (usuário comum, sem admin) ──────────────────────────┐
 │                                                                                          │
 │  Bandeja (ícone + menu) ──► "Abrir painel" ──► navegador em http://127.0.0.1:<porta>     │
 │                                                                                          │
 │  Agendador ──► Sondas (latência, gateway, Wi-Fi, velocidade) ──► SQLite (arquivo local)  │
 │      │            ▲ orçamento de dados                                │                  │
 │      │                                                                ▼                  │
 │      └──► 8h: Análise ──► Relatório diário ──► Notificação do Windows                     │
 │                                                                                          │
 │  Servidor web local (só 127.0.0.1) ──► API JSON + painel (HTML/JS embutido no .exe)      │
 └──────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Um código, dois executáveis** (decisão da Fase 4, como `python.exe`/`pythonw.exe`):
  `avanet.exe` (console, para os comandos) e `avanetw.exe` (compilado com `-H=windowsgui`: sem
  janela, só o ícone na bandeja). Com um único executável de console, o Windows abriria uma
  janela de terminal a cada inicialização. O painel (HTML, CSS, JS) vai embutido com `go:embed`.
- **Inicia com o Windows** pela chave `HKCU\...\Run` apontando para o `avanetw.exe`, sem
  administrador (`avanet autostart on|off`). Aparece em "Aplicativos de inicialização" do
  Gerenciador de Tarefas, onde o usuário também pode desligar.
- **Instância única** por pasta de dados (mutex `Local\` do Windows): abrir de novo avisa que já
  está rodando. A pasta de desenvolvimento (`AVANET_DATA_DIR`) não bloqueia a real.
- **Sessões (cobertura):** cada inicialização abre uma sessão, estendida a cada rodada. Uma pausa
  maior que 3 intervalos (suspensão/hibernação) fecha a sessão e abre outra; ela não conta como
  cobertura nem como queda.
- **Agenda dos testes de velocidade:** mantém o intervalo do perfil enquanto o orçamento do dia
  permite; senão espalha os testes que cabem até a meia-noite (ex.: 13 testes em 16 h → um a
  cada ~74 min), para a noite não ficar sem medição. Um teste extra roda quando a latência
  dobra e sobe 30 ms acima da média recente (no máximo 1 por hora). O agendamento usa o relógio
  do agente e sobrevive a reinícios (último teste lido do banco).

**Por que painel web local e não janela nativa?** O Go não tem uma biblioteca de interface
nativa madura e simples para Windows. HTML+JS dá gráficos bons de graça, é o mesmo modelo do
Syncthing (outro app Go) e é fácil de reaproveitar se um dia virar produto. Alternativa avaliada:
**Wails** (janela nativa com WebView2). Fica como opção se o painel no navegador incomodar.

### Estrutura do repositório

```
avanet/
  cmd/avanet/              # ✅ executável de console (comandos)
  cmd/avanetw/             # ✅ executável sem janela (bandeja; o que inicia com o Windows)
  internal/
    app/                   # ✅ linha de comando e ligação entre os pacotes
    agent/                 # ✅ monitor: rodadas, agenda dos testes, quedas, sessões, teste com orçamento
    buildinfo/             # ✅ versão do executável
    config/                # ✅ configuração do processo (pasta de dados, nível de log), validada
    logging/               # ✅ logs JSON com rotação por tamanho
    domain/                # ✅ tipos e regras puras (latência, rede, Wi-Fi, quedas, perfis, agenda) — sem I/O
    netinfo/               # ✅ interface usada para a internet, gateway, tipo, conexão limitada, contadores
    wifi/                  # ✅ sinal, RSSI, canal, faixa e padrão do Wi-Fi (wlanapi.dll)
    probe/                 # ✅ latência (pro-bing) e velocidade (Cloudflare)
    ptbr/                  # ✅ números no formato brasileiro (vírgula decimal)
    instance/              # ✅ instância única (mutex do Windows)
    autostart/             # ✅ iniciar com o Windows (registro HKCU\...\Run)
    tray/                  # ✅ ícone na bandeja (cor pela qualidade) e menu
    analysis/              # agregação diária, percentis, faixas de horário, padrões, plano contratado
    report/                # monta o relatório diário a partir da análise
    storage/               # ✅ SQLite: migrações, medições, sessões, configurações
    notify/                # notificação do Windows atrás de uma interface Notifier
    web/                   # servidor local, API JSON, segurança (token, Host)
  ui/                      # painel: HTML/CSS/JS + biblioteca de gráficos (embutido no build)
  docs/
  spikes/                  # experimentos da Fase 0, guardados como registro de decisão
  .github/workflows/       # CI: build, lint, testes, vulnerabilidades (runner Windows)
```

**Regras:**
- `domain` não importa nenhum outro pacote do projeto.
- Código específico do Windows fica em arquivos `_windows.go` dentro de cada pacote (padrão do
  Go: o compilador escolhe o arquivo pelo sistema). Cada pacote tem um `_other.go` que devolve
  `ErrUnsupported`, então tudo compila e testa também no Linux (onde roda o detector de
  concorrência do CI). Portar para outro sistema = escrever o arquivo daquele sistema.
  *(Decisão da Fase 2: substitui a pasta única `platform/windows`, que misturaria assuntos
  diferentes num só pacote.)*
- Nova sonda = novo arquivo em `probe/`, sem alterar o agendador (princípio aberto/fechado).

### Modelo de dados

Durações em microssegundos inteiros (`_us`), velocidades em Mbps (`REAL`).

| Tabela | Campos principais |
|---|---|
| `networks` ✅ | id, kind (ethernet/wifi/mobile/other), interface_name, ssid, first_seen_ms — migração 0003 |
| `latency_samples` ✅ | at_ms, network_id, target (gateway/Cloudflare/Google/Quad9), sent, received, median/jitter/min/max_us (NULL sem resposta) |
| `speed_tests` ✅ | at_ms, network_id, trigger (scheduled/adaptive/manual), server, idle_latency_us, download/upload_mbps, bytes_down/up, *_capped, local_traffic |
| `wifi_snapshots` ✅ | at_ms, network_id, signal_percent, rssi_dbm, channel, band, standard, rx_rate_mbps |
| `outages` ✅ | network_id, started_ms, ended_ms (NULL enquanto dura) |
| `agent_sessions` ✅ | started_ms, last_ms — períodos contínuos de execução (cobertura) |
| `data_usage` ✅ | day (YYYY-MM-DD local), data_class (fixed/mobile), bytes — migração 0002 |
| `settings` ✅ | key, value (`profile` na Fase 4) |
| `daily_reports` | date, profile, summary_json, generated_at, notified_at (Fase 5) |

Volume: com o perfil Completo, ~5.800 linhas de latência por dia (alguns MB por mês). Uma
rotina de limpeza/compactação do histórico antigo fica para quando houver relatórios (Fase 5).

Horários guardados como **inteiros em milissegundos UTC** (colunas com sufixo `_ms`):
compactos, rápidos para consultas por intervalo e convertidos para o fuso local só na exibição
e na separação por dia. Tabelas `STRICT` (o SQLite valida os tipos).

## 9. Stack

| Camada | Escolha |
|---|---|
| Linguagem | Go 1.27 |
| Banco | ✅ SQLite via `modernc.org/sqlite` (Go puro, sem compilador C), modo WAL |
| Migrações | ✅ Migrador próprio (~150 linhas): SQL numerado embutido no binário, checksum de cada migração aplicada, recusa banco de versão mais nova. Preferido ao goose por ser pequeno e sem dependências. |
| Logs | ✅ `log/slog` (JSON) com rotação própria por tamanho (5 MB × 4 arquivos) |
| Latência | `pro-bing` (ICMP, sem admin); `IcmpSendEcho` como plano B |
| Velocidade | ✅ Cliente HTTP próprio contra a Cloudflare: sem redirecionamentos, sem compressão, HTTPS obrigatório, nunca lê além do tamanho pedido; upload gerado na hora (ChaCha8), sem buffer em memória |
| Rede / Wi-Fi | `golang.org/x/sys/windows` + `wlanapi.dll` + `INetworkCostManager` (COM) |
| Bandeja | `fyne.io/systray` |
| Notificação | `git.sr.ht/~jackmordaunt/go-toast/v2` (WinRT via COM) |
| Painel | HTML/CSS/JS sem framework, gráficos em SVG próprio (ou uPlot se ficar pesado), fontes embutidas |
| Qualidade | ✅ `gofmt`/`goimports`, `golangci-lint` v2 (18 linters, incl. `gosec`), `go test`, `govulncheck`, `gitleaks` |
| CI | ✅ GitHub Actions: Windows (lint, testes, build, vulnerabilidades) + Linux (race detector) + varredura de segredos. Actions fixadas por SHA. |
| Distribuição | `.zip` com o executável nas releases do GitHub; instalador só se virar produto |

## 10. Padrões de engenharia

### Código limpo
- Nomes claros, funções pequenas, sem código morto.
- Erros **sempre tratados ou propagados com contexto** (`fmt.Errorf("...: %w", err)`), nunca ignorados.
- Configuração validada na inicialização; sem números mágicos espalhados (limiares e intervalos
  ficam nos perfis).
- Desligamento limpo: ao sair, o agente termina a medição atual e fecha o banco
  (`context.Context` em todas as operações longas).
- Identificadores e código em inglês; interface em português.

### Proteções da fundação (Fase 1)

O que impede que uma alteração futura quebre o app em silêncio:

- **Migrações somente acréscimo:** o checksum de cada migração aplicada fica no banco. Editar uma
  migração antiga faz o app recusar iniciar (em vez de deixar bancos diferentes entre versões).
  Banco migrado por uma versão mais nova também é recusado. Cada migração roda numa transação:
  se falhar, nada fica pela metade.
- **Verificação de integridade** do banco (`PRAGMA quick_check`) ao iniciar.
- **Configuração validada na inicialização** (caminhos absolutos, nível de log válido).
- **Interrupção segura:** Ctrl+C ou desligamento do Windows encerram limpo, inclusive durante a
  inicialização.
- **`.\scripts\check.ps1`** roda localmente exatamente o que o CI roda; o CI bloqueia o merge.
- **Line endings fixados** (`.gitattributes`) para o Git no Windows não alterar arquivos.

### Testes
- Regras de domínio e análise (percentis, faixas de horário, padrões, nota do dia) com testes
  unitários e **tabelas de casos** (padrão do Go).
- Sondas testadas contra servidores falsos locais (`httptest`), sem internet no CI.
- Relógio injetável para testar agendamento e o "8h" sem esperar.

### Fluxo de trabalho
- Branch `main` protegida, branches curtas + pull request, *Conventional Commits*.
- CI verde obrigatório antes do merge.

### Segurança e privacidade
- **Sem privilégio de administrador.** Roda como usuário comum.
- **Servidor local só em `127.0.0.1`**, porta aleatória, **token secreto** na URL aberta pela
  bandeja. Toda requisição sem token é recusada. Também valida o cabeçalho `Host` (contra *DNS
  rebinding*), não usa CORS e manda CSP restritiva no painel.
- **Destinos de rede fixos** (allowlist): o app só conversa com os servidores de teste e os alvos
  de latência configurados no código, sempre via HTTPS com verificação de certificado.
- **Comandos do sistema** (se algum for necessário) chamados com caminho absoluto e argumentos
  fixos, nunca por shell e nunca com texto vindo de fora.
- **Privacidade:** nada sai do computador além do tráfego dos próprios testes. Sem telemetria.
  **O IP público não é armazenado.** O usuário pode apagar todo o histórico pela interface.
- Dependências fixadas, Dependabot, `govulncheck` e `gitleaks` no `check.ps1` e no CI.
- DLLs do Windows carregadas só da pasta `System32` (`NewLazySystemDLL`), contra sequestro de DLL.

## 11. Aprendendo Go durante o projeto

O assistente explica cada trecho novo de Go conforme ele aparece no código. Para acompanhar:
- **Tour of Go** (go.dev/tour): tutorial interativo oficial. Faça antes da Fase 2 (~1 tarde).
- **Go by Example** (gobyexample.com): consulta rápida de "como faz X em Go".

Conceitos que vão aparecer, em ordem: pacotes e `go mod` → structs e métodos → tratamento de
erro (`if err != nil`) → interfaces → goroutines e canais (as sondas rodam em paralelo) →
`context` (cancelar e limitar tempo) → testes com tabelas.

## 12. Roadmap

| Fase | Entrega |
|---|---|
| **0 — Spike** ✅ | Experimentos no Windows do dono: latência sem admin, gateway, velocidade (Cloudflare), Wi-Fi, conexão limitada, bandeja e notificação. Tudo viável; resultado na seção 3. |
| **1 — Fundação** ✅ | Repositório, módulo Go, CLI (`run`, `version`, `help`), configuração validada, logs com rotação, SQLite com migrações e verificação de integridade, testes, lint, `check.ps1`, CI, Dependabot. Conceito visual do painel. |
| **2 — Sondas leves** ✅ | Latência até o roteador e 3 alvos (Cloudflare, Google, Quad9) em paralelo, com mediana, jitter e perda; "latência da internet" = melhor alvo. Detector de quedas (2 rodadas seguidas sem resposta). Rede: interface realmente usada (`GetBestInterfaceEx`), gateway, tipo, conexão limitada. Wi-Fi: sinal, RSSI, canal, faixa (pela frequência do ponto de acesso) e padrão. Comando `avanet probe`. Gravação no banco fica com o agente (Fase 4). |
| **3 — Velocidade** ✅ | Download/upload em degraus contra a Cloudflare, com correção da aceleração do TCP (download) e relógio do servidor (upload); latência pelo handshake TCP; orçamento diário gravado no banco; rede móvel bloqueada sem consentimento; detecção de uso local. Comando `avanet speedtest [-movel]`. Achados na seção 3. |
| **4 — Agente** ✅ | Agente que mede no intervalo do perfil e grava tudo (latência, Wi-Fi, velocidade, quedas, sessões); agenda dos testes que espalha o orçamento; teste extra quando a latência piora; suspensão não vira queda; bandeja com ícone colorido pela qualidade e "Testar velocidade agora"; `avanetw.exe` sem janela; iniciar com o Windows; instância única. Comandos `run [-tray]`, `status`, `profile`, `autostart`. |
| **5 — Relatório diário** | Análise do dia, relatório às 8h, recuperação ao ligar, notificação. |
| **6 — Painel** | Servidor local seguro, tela inicial (status agora), aba Relatórios com gráficos, escolha de perfil com descrições, configurações (orçamento, rede móvel, plano contratado). |
| **7 — Inteligência** | Padrões de 3+ dias com nível de confiança, comparação com o plano contratado (Anatel). |
| **8 — Portfólio** | README com prints e diagrama, release no GitHub, documento de arquitetura, repositório público. |

## 13. Backlog de baixa probabilidade

- Juntar dados de vários dispositivos num lugar só (exigiria servidor/sincronização).
- Versão Android (tarefas a cada 15 min no mínimo e fabricantes que encerram apps em segundo plano).
- Linux/macOS.
- Virar produto: freemium (histórico longo, PDF para reclamação na Anatel), assinatura digital do
  executável, instalador, Microsoft Store.
- Exportar CSV/PDF.

## 14. Pré-requisitos (ações do dono do projeto)

- [x] Instalar **Go** e **Git**.
- [ ] **VS Code** com a extensão oficial de Go (o repositório já recomenda a extensão).
- [ ] Fazer o **Tour of Go** — antes da Fase 2.
- [ ] Criar o repositório no GitHub (`avanet`) e enviar o código — o CI só roda depois disso.

## 15. Perguntas em aberto

- [ ] Repositório público desde o início ou privado até a Fase 8?
- [ ] **Licença** do código. Sem licença = todos os direitos reservados, o que mantém o "talvez
  produto" em aberto; MIT/Apache facilita o portfólio. Decidir antes de tornar público.
- [ ] Confirmar a regra atual da Anatel (RQUAL, Resolução 717/2019) antes da Fase 7.
- [ ] A notificação de teste da Fase 0 apareceu na tela do dono?
- [ ] Testar a detecção de conexão limitada com um hotspot de celular real (Fase 3).
