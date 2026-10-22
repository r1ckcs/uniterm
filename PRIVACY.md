# Política de privacidade do FlexTerm

*Última atualização: 22/10/2026*

O FlexTerm é um terminal SSH/Telnet/SFTP de uso interno, derivado do uniTerm. Esta página descreve o que o app faz com dados da sua Conta do Google quando você liga o cofre de credenciais.

## O que o app acessa

- O app pede um único escopo do Google: `https://www.googleapis.com/auth/drive.file`.
- Com ele, o FlexTerm só enxerga **os arquivos que ele mesmo criou** no seu Google Drive (a pasta `FlexTerm Vault`). O restante do seu Drive, Gmail, contatos ou qualquer outro dado da conta não é acessado.

## O que é guardado no Google Drive

- Somente arquivos **cifrados no próprio aparelho** antes do envio (conexões, grupos e identidades do cofre).
- As chaves que abrem o cofre ficam nos aparelhos (Keychain do sistema) ou nas 24 palavras de recuperação. Elas nunca são enviadas ao Google nem a terceiros.

## O que não é feito

- Nenhum dado é enviado a servidores do FlexTerm ou de terceiros: o app conversa direto com o Google Drive.
- Não há publicidade, rastreamento, venda ou compartilhamento de dados.
- O login com Google (OAuth com PKCE e retorno só em `127.0.0.1`) serve apenas para liberar o acesso ao Drive; ele não abre o cofre.

## Como revogar

- Em <https://myaccount.google.com/permissions>, remova o acesso do FlexTerm a qualquer momento.
- Para apagar os dados, exclua a pasta `FlexTerm Vault` do seu Drive.

## Contato

Dúvidas: henrique.souza@internetflex.com.
