# Defense Bridge Client

Cliente de teste e homologacao em Go para autenticar no Bridge do Defense IA, manter a sessao ativa e publicar eventos customizados por uma interface web simples.

## Objetivo

Este projeto reduz o custo de validar integracoes com o Bridge. Em vez de montar chamadas HTTP manualmente, o operador informa os parametros na UI e o backend cuida do ciclo de vida da sessao.

## Responsabilidades do sistema

- autenticar com `accessKey` + `secretKey` usando `HMAC-SHA256`
- manter a sessao viva com `heartbeat`
- renovar token periodicamente
- reautenticar uma vez quando uma chamada autenticada recebe `401`
- consultar a configuracao de um Bridge por ID
- enviar eventos customizados com defaults aplicados no servidor
- armazenar logs operacionais em memoria para troubleshooting rapido

## Arquitetura

### `main.go`

Faz o bootstrap da aplicacao HTTP e define o endereco de escuta.

### `internal/server`

Responsavel pela camada web:

- renderizacao do template principal
- exposicao dos assets estaticos
- API JSON consumida pelo front-end

### `internal/bridge`

Responsavel pela integracao com a API remota:

- validacao e normalizacao de configuracao
- autenticacao e extracao de token
- heartbeat e token update em background
- envio de eventos e consulta de configuracao do Bridge
- log operacional em memoria

## Fluxo operacional

### 1. Authorize

Endpoint padrao:

`POST /ecos/api/v1.1/account/authorize`

Payload enviado:

```json
{
  "accessKey": "...",
  "signature": "hmac_sha256(timestamp, secretKey)",
  "timestamp": "1712499999999"
}
```

### 2. Heartbeat

Endpoint padrao:

`POST /ecos/api/v1.1/account/heartbeat`

Frequencia padrao: a cada `30` segundos.

### 3. Token update

Endpoint padrao:

`POST /ecos/api/v1.1/account/token/update`

Frequencia padrao: a cada `25` minutos.

### 4. Bridge config

Endpoint padrao:

`GET /ecos/api/v1.1/bridge/{id}/config`

O placeholder `{id}` e resolvido com o `bridgeId` informado na interface.

### 5. Event push

Endpoint padrao:

`POST /ecos/api/v1.1/bridge/event/push`

Payload minimo montado pelo backend:

```json
{
  "eventId": "uuid-like-gerado-pelo-servidor",
  "eventTime": 1712499999,
  "eventSourceCode": "1000005$1$0$2",
  "eventSourceName": "Entrance Door",
  "eventTypeCode": "51",
  "eventTypeName": "Valid Swipe",
  "remark": "Mensagem enviada pela interface"
}
```

Campos extras enviados pela UI sao mesclados ao payload final desde que `extraJson` seja um objeto JSON valido.

## Como executar

### Requisitos

- Go instalado
- acesso ao ambiente que expoe a API do Defense IA

### Execucao local

```bash
go run .
```

Endereco padrao:

`http://localhost:8080`

Para alterar a porta:

```bash
APP_ADDR=:9090 go run .
```

## Contratos e convencoes importantes

- o token pode vir no header `X-Subject-Token` ou em campos conhecidos no JSON de resposta
- `timestamp` do authorize usa Unix em milissegundos
- `eventTime` usa Unix em segundos
- endpoints e intervalos podem ser sobrescritos pela interface
- quando a API responde `401`, o cliente tenta reautenticar e repetir a operacao uma unica vez
- os logs ficam apenas em memoria; reiniciar o processo limpa o historico

## Diretriz de manutencao

- documente intencao, contratos e regras de negocio; evite comentar o obvio
- mantenha a API local retornando erros no formato `{"error":"..."}` para simplificar o front-end
- preserve a separacao entre camada HTTP (`internal/server`) e integracao remota (`internal/bridge`)
