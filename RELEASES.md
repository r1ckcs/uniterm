# Releases assinadas do fork

O atualizador do app (`backend/update`) só instala uma release de `github.com/r1ckcs/uniterm` quando o `checksums.txt` dela traz um `checksums.txt.sig` ed25519 válido para a **chave pública embutida no build**. Um build sem chave avisa que existe versão nova, mas nunca instala nada (fail-closed).

A **chave privada nunca toca este Mac** (houve malware nele em 21/10/2026) nem nenhuma máquina com rede. Só dois arquivos pequenos atravessam entre as máquinas: `checksums.txt` vai para a máquina offline e `checksums.txt.sig` volta.

## 1. Gerar a chave (uma vez, na máquina offline)

1. Use uma máquina limpa: live USB de Linux (Debian/Ubuntu live; OpenSSL ≥ 3 já vem) ou um computador que nunca teve as contas do Henrique. **Deixe a rede desligada** (sem Wi-Fi, sem cabo) do boot até desligar.
2. Leve `scripts/release/offline-keygen.sh` e `offline-sign.sh` num pendrive (pendrive A, só os scripts). Confira: são shell legível, sem download.
3. Num **pendrive B**, que vai guardar a chave e nunca será plugado em máquina com rede:
   ```sh
   sh offline-keygen.sh /media/<pendrive-B>/uniterm-release
   ```
   Escolha uma frase-senha forte. A chave privada sai cifrada (AES-256) em `uniterm-release.key`, com modo 0600.
4. Faça uma **segunda cópia** do `uniterm-release.key` em outro pendrive offline, guardado em outro lugar. Perder a chave = ter que publicar uma build nova com outra chave pública, e os apps antigos não aceitariam mais atualização.
5. Anote a **fingerprint** que o script mostra (papel).
6. Copie **só** o `release-pubkey.txt` para o pendrive A.

## 2. Pôr a chave pública no repo (neste Mac)

```sh
cp /Volumes/<pendrive-A>/release-pubkey.txt build/release-pubkey.txt
go run ./scripts/release/sigtool fingerprint -pub @build/release-pubkey.txt   # tem que ser igual à anotada
git add build/release-pubkey.txt && git commit -m "build: add release signing public key"
```

A partir daí, todo build de produção (`wails3 task package`/`build`) embute a chave via `-ldflags -X github.com/ys-ll/uniterm/backend/update.releasePublicKey=…`. **Releases só com build nativo** (o build por Docker não recebe a chave).

## 3. Fazer uma release

Versão nova precisa ser maior que a instalada, em SemVer. O texto depois de `+` não conta, então de `1.9.5+security` vá para `1.9.6`.

```sh
export PATH="$HOME/go/bin:$PATH"
wails3 task release:package VERSION=1.9.6
```

Isso faz:
- o build de arm64 e amd64;
- **recusa** binário que não embute a chave (`sigtool binkey`);
- gera `dist/release/uniterm-darwin-<arch>-v1.9.6.zip`, com o executável na raiz, que é o formato que o atualizador extrai;
- gera `dist/release/checksums.txt`.

Leve `dist/release/checksums.txt` para a máquina offline (pendrive A) e assine lá:

```sh
sh offline-sign.sh /media/<pendrive-B>/uniterm-release/uniterm-release.key checksums.txt
```

O script mostra a lista, pede confirmação (confira nomes e quantidade), assina e confere a assinatura. Ele recusa linha que não seja `<sha256>  uniterm-<arquivo>`. Traga `checksums.txt.sig` de volta para `dist/release/`.

```sh
wails3 task release:verify                       # assinatura + hash de cada asset
wails3 task release:publish VERSION=1.9.6        # cria a release como RASCUNHO em r1ckcs/uniterm
```

Confira o rascunho no GitHub (assets e notas) e clique em **Publish**. O app só enxerga releases publicadas (`releases/latest`). Notas próprias: `NOTES_FILE=notas.md`.

## Se a chave vazar ou se perder

- **Vazou:** gere uma chave nova (passo 1), troque `build/release-pubkey.txt`, publique uma release assinada com a **chave antiga** contendo o build com a chave nova (assim os apps existentes migram), e apague a chave antiga. Se a chave antiga não existir mais, quem tem o app precisa reinstalar à mão.
- **Perdeu:** igual a "vazou", mas sem a release de transição: a reinstalação manual é obrigatória.

## Ferramentas

| Arquivo | Onde roda | Faz |
|---|---|---|
| `scripts/release/offline-keygen.sh` | máquina offline | gera o par; imprime a fingerprint |
| `scripts/release/offline-sign.sh` | máquina offline | assina `checksums.txt` depois de mostrar e confirmar |
| `scripts/release/sigtool` (Go) | este Mac | `checksums`, `verify`, `binkey`, `fingerprint`; nunca lê chave privada |
| `Taskfile.yml` `release:*` | este Mac | empacota, verifica e publica (rascunho) |

`scripts/sync-release-to-gitee.sh` é do upstream e **não** deve ser usado: o fork não publica no Gitee.

Testes: `go test ./scripts/release/... ./backend/update/` cobre:
- o `-X` chega ao binário;
- os Taskfiles usam o símbolo certo;
- os scripts `openssl` interoperam com a verificação do app;
- asset adulterado ou fora da lista falha;
- build sem chave recusa tudo.
