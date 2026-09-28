# Correções de segurança deste fork

Fork de [ys-ll/uniterm](https://github.com/ys-ll/uniterm) (base: `f95a1b30`), auditado e corrigido em 28/09/2026.
Branch: `security/hardening`.

## Corrigido

| Sev. | Problema | Correção |
|---|---|---|
| Crítica | **SSH não verificava a chave do servidor** (`InsecureIgnoreHostKey` em 11 pontos): MITM capturava senha e sessão | `known_hosts` próprio (0600) + leitura de `~/.ssh/known_hosts`; chave desconhecida pede confirmação com fingerprint SHA256; chave **alterada** é sempre recusada; teste de conexão nunca aceita chave nova |
| Alta | **Path traversal em download recursivo** (SCP/SFTP/FTP/SMB/WebDAV/S3): servidor malicioso gravava `../../.ssh/authorized_keys` | nomes remotos precisam ser um único componente local; SCP aborta (classe CVE-2019-6111) |
| Alta | **Zip-slip na importação de skill**: zip gravava em qualquer caminho do usuário | `filepath.IsLocal` + recusa de symlink |
| Alta | **Atualizador sem verificação obrigatória**: sem `checksums.txt`, instalava o binário sem conferir; frontend podia injetar URL+hash próprios | fail-closed sem sha256; só HTTPS (inclusive redirects); `DownloadUpdate` só aceita assets vindos do `CheckForUpdate` do backend |
| Alta | **Agente de IA executava comandos sem aprovação**: `send_terminal_key` não passava pelo gate; risco era o rótulo do próprio modelo (forjável por prompt injection) | teclas digitadas passam pela política; risco = máx(rótulo do modelo, classificador local) |
| Alta | **Exportação `.utm` vazava chaves privadas** (`KeyContent`), senha do Sentinel e do túnel em texto puro, mesmo com senha | todos os campos secretos são limpos/cifrados |
| Alta | **Injeção de comando no editor externo (Windows, `.cmd`/BatBadBut)** via nome de arquivo remoto `a&calc&.txt` | metacaracteres do cmd.exe removidos do nome temporário e recusados em editor `.cmd/.bat` |
| Alta | **RDP alterava o registro do Windows globalmente** (`AuthenticationLevelOverride=0`, política de aviso, `reg.exe` elevado em HKLM), silenciando avisos do `mstsc` fora do app | escritas removidas |
| Média | Classificador de risco do MCP contornável (`&&`, `$( )`, `bash -c`, `find -delete`, `curl \| sh`) e panic com `sudo` isolado | split em `&`, substituição de comando/eval/xargs/interpretadores = perigoso; política desconhecida = confirmar; `download_file` perigoso; leitura remota passa pelo gate |
| Média | Link `java<TAB>script:` passava pelo sanitizador de markdown; imagem remota carregava sozinha (exfiltração) | allowlist de esquemas após normalização; imagens viram link; links abrem só http(s)/mailto no navegador do sistema |
| Média | `save_skill` persistia prompt injection sem aprovação; `CreateSkill` sobrescrevia e destravava skill travada | exige aprovação; `CreateSkill` não sobrescreve |
| Média | Senha mestra: qualquer senha "desbloqueava" e virava a chave de auto-desbloqueio | valor de verificação no `credentials.meta` (e teste de decifração em stores antigos) |
| Média | Passphrase de chave, API key do Elasticsearch, senha do Sentinel e do túnel em texto puro no `connections.json` | cifrados em repouso; migração automática ao carregar; sync normaliza os mesmos campos |
| Média | SQL Server com `encrypt=disable` (senha praticamente em claro) | padrão `encrypt=false` (TLS no login); parâmetros do usuário prevalecem |
| Média | Diretório do editor externo em `/tmp` previsível e 0644 (Linux multiusuário) | cache do usuário, 0700/0600 |
| Baixa | Token do sync aceito em `http://` | só `https://` quando há token |
| Baixa | Log do app e transcrições de sessão legíveis por todos | 0600 em diretório 0700 |
| Baixa | Nome de unit systemd podia começar com `-` (injeção de opção) | primeiro caractere alfanumérico |
| Baixa | Panic no MCP quando `mcp.json` não existe | corrigido |

## Não corrigido (e por quê)

- **Assinatura das atualizações**: o `checksums.txt` vem da mesma release do binário. Proteger contra conta/CI comprometidos exige uma chave ed25519 mantida offline por quem publica as releases. Se este fork publicar releases próprias, apontar o updater para o fork e assinar.
- **RDP**: a verificação de certificado ainda fica a cargo do ActiveX, e o app clica sozinho nos diálogos de segurança do próprio processo. `RedirectDrives` segue ligado. Mudar isso exige testar em Windows.
- **AAD por arquivo no sync**: vincular o texto cifrado ao nome do arquivo quebraria aparelhos rodando a versão original no mesmo repositório.
- **CSP no frontend**: sem rodar o app, uma CSP restritiva pode quebrar a UI. Recomendada como próximo passo.
- **Chave mestra em cache no keychain** (modo senha mestra): comportamento documentado do upstream.
- Variáveis `UNITERM_UPDATE_API_BASE`/`UNITERM_UPDATE_AUTOTEST` ativas em produção: quem controla o ambiente do usuário já executa código.

## Compatibilidade

- Na primeira conexão SSH após atualizar, cada host pede confirmação de chave (ou é aceito direto se já estiver em `~/.ssh/known_hosts`).
- `connections.json` passa a cifrar passphrases e senhas auxiliares: voltar para a versão original deixaria esses campos ilegíveis.
- SQL Server sem suporte a TLS algum: adicione `encrypt=disable` nos parâmetros extras.

## Validação

- `go test ./...`: 15 pacotes passando; `GOOS=windows go build ./...` ok.
- Frontend (`vitest`): 554 testes passando. As 6 falhas também existem no upstream (bindings Wails não gerados e um teste de paridade de settings).
