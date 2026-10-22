# Integração com o Google (cofre de credenciais)

O cofre do FlexTerm usa o Google Drive apenas para guardar arquivos **cifrados no próprio aparelho**. O login com Google libera o acesso ao Drive, mas **não abre o cofre**: para isso é preciso a chave do aparelho, a aprovação de um aparelho que já está no cofre ou as 24 palavras de recuperação.

O app pede um único escopo, `drive.file`, que dá acesso **apenas aos arquivos que o próprio FlexTerm criou** (a pasta `FlexTerm Vault`). O app não vê o restante do Drive.

Para subir a integração, você cria um projeto no Google Cloud com um cliente OAuth do tipo "App para computador" e entrega o JSON desse cliente ao app. São uns 10 minutos.

## 0. Qual conta usar

| Conta | Tipo de app | Quem pode entrar | Observação |
|---|---|---|---|
| **Google Workspace da empresa** (recomendado) | **Interno** | qualquer conta do domínio | sem verificação do Google e sem limite de usuários de teste; o cofre do NOC precisa disto |
| Gmail pessoal | Externo | só os "usuários de teste" cadastrados (até 100) | em modo **Teste**, o login **expira a cada 7 dias** (veja a seção 6) |

Entre no console com a conta escolhida **antes** de criar o projeto. O projeto fica preso à organização dessa conta.

## 1. Criar o projeto

1. Abra <https://console.cloud.google.com>.
2. No seletor de projeto (alto, à esquerda), clique em **Novo projeto**.
3. Preencha:
   - **Nome:** `FlexTerm`;
   - **Organização:** a da empresa, se aparecer.
4. Clique em **Criar** e confirme que o `FlexTerm` ficou selecionado no topo.

## 2. Ativar a API do Drive

1. Menu ☰ → **APIs e serviços** → **Biblioteca**.
2. Busque **Google Drive API** → **Ativar**.

## 3. Tela de consentimento

Fica em menu ☰ → **APIs e serviços** → **Tela de consentimento OAuth**. Em versões novas do console, aparece como **Google Auth Platform**.

1. Clique em **Começar** (ou **Configurar**).
2. Preencha:
   - **Nome do app:** `FlexTerm`;
   - **E-mail de suporte:** o seu.
3. Em **Público**, escolha:
   - **Interno**, se for conta Workspace;
   - **Externo**, se for Gmail.
4. Em **Dados de contato**, informe o seu e-mail, aceite a política e conclua.
5. Em **Acesso a dados** (ou **Escopos**) → **Adicionar ou remover escopos**:
   - marque **somente** `https://www.googleapis.com/auth/drive.file`;
   - salve.

   Não adicione outros escopos: `drive.file` não é sensível e dispensa a verificação do Google.
6. **Somente se escolheu Externo:** em **Público** → **Usuários de teste**, adicione o seu e-mail e o de cada colega que vai testar.

## 4. Criar o cliente OAuth

1. Menu ☰ → **APIs e serviços** → **Credenciais** → **+ Criar credenciais** → **ID do cliente OAuth**.
2. Preencha:
   - **Tipo de aplicativo:** **App para computador**;
   - **Nome:** `FlexTerm Desktop`.
3. Clique em **Criar** e, na janela que abrir, em **Fazer o download do JSON**.

   O arquivo baixado se chama `client_secret_<...>.json` e tem a forma `{"installed": {"client_id": "...", "client_secret": "...", ...}}`. O FlexTerm lê esse formato diretamente.

Em app de computador, o `client_secret` **não é segredo de verdade**: o Google sabe que ele vai dentro do app. A segurança do login vem de três coisas:

- **PKCE (S256)**;
- **`state` conferido**;
- **retorno só em `127.0.0.1`**.

Mesmo assim, o arquivo **não entra no repositório** (o repo é público).

## 5. Entregar o cliente ao FlexTerm

O app procura o cliente em `google-oauth.json`, dentro da **pasta de dados padrão**:

| Sistema | Caminho |
|---|---|
| macOS | `~/Library/Application Support/FlexTerm/google-oauth.json` |
| Windows | `%AppData%\FlexTerm\google-oauth.json` |
| Linux | `~/.config/FlexTerm/google-oauth.json` |

A pasta padrão vale mesmo quando você testa com `UNITERM_DATA_DIR`, então os "aparelhos" de teste usam o mesmo cliente.

No Mac, guarde o original em `~/.credenciais` (o lugar padrão das credenciais) e aponte o app para ele:

```sh
mkdir -p ~/.credenciais && chmod 700 ~/.credenciais
mv ~/Downloads/client_secret_*.json ~/.credenciais/google-flexterm.json
chmod 600 ~/.credenciais/google-flexterm.json
mkdir -p ~/Library/Application\ Support/FlexTerm
ln -sf ~/.credenciais/google-flexterm.json ~/Library/Application\ Support/FlexTerm/google-oauth.json
```

Se o arquivo não existir, a tela de boas-vindas mostra o erro `vault.oauth.noClientId`. Nesse caso, só o modo de teste com pasta local funciona.

**Build para distribuir à equipe** (opcional, para ninguém precisar copiar o JSON): embuta o cliente no binário por `-ldflags`:

```
-X github.com/ys-ll/uniterm/backend/vault/oauth.DefaultClientID=<client_id>
-X github.com/ys-ll/uniterm/backend/vault/oauth.DefaultClientSecret=<client_secret>
```

O `google-oauth.json`, quando existe, tem prioridade sobre o valor embutido.

## 6. Primeiro login

1. Abra o FlexTerm e clique em **Entrar com Google para sincronizar**.
2. O navegador do sistema abre na página do Google. Escolha a conta e autorize o acesso aos arquivos criados pelo FlexTerm.
3. Volte ao app:
   - **1º aparelho:** o cofre é criado e o app mostra as **24 palavras de recuperação**. Anote-as no papel e confirme as 3 que o app pedir.
   - **Aparelho novo:** o app fica em "Aguardando aprovação". No aparelho que já está no cofre, aprove: um aparelho mostra 6 dígitos para o outro digitar, e depois o caminho inverso com 4 dígitos.

O token de acesso fica no **Keychain** do aparelho, nunca em arquivo, e é revogado ao sair do cofre.

**Expiração em 7 dias (somente app Externo em modo Teste).** O Google invalida o login a cada 7 dias, e o app volta a pedir para entrar com Google. O cofre e os dados não se perdem. Para acabar com isso, publique o app: **Público** → **Publicar app** → **Em produção**. Como o único escopo é `drive.file`, que não é sensível, a publicação não exige verificação. App **Interno** não tem essa limitação.

## 7. Testar dois aparelhos no mesmo Mac

**Com Google:**

```sh
UNITERM_DATA_DIR=$HOME/flexterm-teste/a /Applications/FlexTerm.app/Contents/MacOS/FlexTerm &
UNITERM_DATA_DIR=$HOME/flexterm-teste/b /Applications/FlexTerm.app/Contents/MacOS/FlexTerm &
```

Cada pasta funciona como um aparelho diferente, com chave própria. Entre com a **mesma conta Google** nos dois e aprove o B a partir do A.

**Sem Google:** use o modo de teste com pasta local, `UNITERM_VAULT_STORE=file:<pasta>`. O script `~/uniterm-equipe/testar-cofre.sh` faz isso. O app mostra a faixa âmbar "Modo de teste".

## 8. Equipe do NOC (fase 4)

O cofre compartilhado do NOC vai morar num **Drive compartilhado** do Workspace, com o app **Interno**. Para essa fase:

- crie o Drive compartilhado `NOC-FlexTerm`;
- adicione os colegas;
- defina quem é administrador. Tenha pelo menos 2 administradores, para o cofre não depender de um único aparelho.

## Problemas comuns

| Sintoma | Causa | O que fazer |
|---|---|---|
| `vault.oauth.noClientId` | falta o `google-oauth.json` (ou o link aponta para arquivo inexistente) | refazer a seção 5 |
| Google mostra "Acesso bloqueado: app não concluiu a verificação" | conta fora da lista de usuários de teste (app Externo) | adicionar a conta em **Usuários de teste**, ou publicar o app |
| `vault.oauth.denied` | consentimento recusado ou conta bloqueada pelo admin do Workspace | autorizar de novo; no Workspace, liberar o app em Admin → Segurança → Controles de API |
| `vault.oauth.timeout` | o navegador não voltou em 5 minutos | tentar de novo; um bloqueador pode estar barrando `127.0.0.1` |
| Pede login de novo toda semana | app Externo em modo Teste | publicar o app (seção 6) |
| `redirect_uri_mismatch` | o cliente foi criado como "Aplicativo da Web" | criar outro do tipo **App para computador** |
