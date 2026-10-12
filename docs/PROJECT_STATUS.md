# ALC / Keygate - suivi du projet de licences

Document de suivi (pas de secret, pas de cle de licence). A actualiser seulement quand c'est utile.
Derniere mise a jour : 2026-10-11 (CDC-KEYGATE-02), branche `feature/alc-license-key-format`.

## 1. Objectif

Licence commerciale pour Autotune Live Control (ALC), environ 29,99 EUR : achat perpetuel, activation initiale en ligne,
utilisation hors ligne permanente ensuite (jetons signes), une seule machine a la fois, transfert controle, gestion
(suspension / revocation) cote serveur. Paiement Stripe plus tard ; aucun paiement reel ni licence client pendant les tests.

## 2. Etat VERIFIE (observe dans le code ou par un test execute)

### Depots et branches
| Element | Etat |
|---|---|
| Depot Keygate local | `/Users/user/Downloads/Keygate-ALC`, origine `tabloy/keygate` |
| Base de la branche | `4c27a4d` (HEAD de `main` local au moment de l'audit, arbre propre) |
| Branche de travail | `feature/alc-license-key-format` ; poussee sur le fork `ddbdjn488g-alt/Keygate-ALC` (remote `fork`), VERIFIE par `git ls-remote` ; `main` du fork inchange (`4c27a4d`) ; rien pousse sur `origin` (`tabloy/keygate`) |
| Commits de la branche | `cc35ede` (format de cles), `4dd4859` (tests d'integration), `5f53c3d` et suivants (ce fichier de suivi) |
| Depot ALC | non modifie par ce travail |

### Ce qu'ALC fait aujourd'hui (lecture de `Source/LicenseManager.h`, `Source/LicenseScreen.h`)
- Format de cle attendu : 18 caracteres, prefixe `ALC-`, tirets aux index 3, 8, 13, lettres/chiffres majuscules.
- Checksum : le 3e groupe est derive des groupes 1 et 2 (hash uint32 grain 0x5A3C, `hash*33 + c`, `^= 0xB7E1`, 4 chiffres base 34 `0-9A-Z` sans I et O).
  Il est verifie a l'activation ET au chargement de l'etat enregistre (une cle invalide est effacee).
- L'activation est locale : ALC n'appelle aucune API Keygate. Verrouillage a la machine via l'identifiant materiel local.
- Constat : `openPurchasePage()` contient un lien Stripe de test code en dur (a remplacer avant production ; non modifie ici).

### Ce que fait Keygate (lecture de `internal/license/keygen.go`, `internal/store/store.go`)
- Cles par defaut `KG-` + 4 segments de 8 caracteres (alphabet de 32, `crypto/rand`, 160 bits).
- Stockage : `license_key` (UNIQUE), `key_hash` (SHA-256 de la cle normalisee, index unique), copie AES-GCM optionnelle ; recherche par hash.
- Deux points de creation de cles : creation admin (`handler/admin.go`) et fulfilment Stripe (`payment/stripe.go`).

### Modification preparee (branche isolee)
- `GenerateALCKey`, `ALCChecksum`, `ValidALCKey`, `KeyPolicy` (format par identifiant de produit reel), `Store.NewLicenseKey`
  (nouvelle tentative bornee a 8 en cas de collision), variable `ALC_KEY_PRODUCT_IDS`, documentation `docs/alc-license-key-format.md`.
- Aucune migration. Licences existantes et format `KG-` inchanges. Autres produits inchanges.

### Tests REELLEMENT executes
| Type | Resultat |
|---|---|
| Unitaires (format, checksum, unicite, rejets, politique par produit, retries, config) | OK |
| Compatibilite : 20 000 cles Go passees dans une transcription C++ du validateur ALC | 20 000 acceptees, 0 refusee (ce n'est pas le binaire JUCE d'ALC) |
| Integration PostgreSQL 16.9 TEMPORAIRE locale (pas la base Blitz) : creation, recherche par hash, unicite, activation, creation admin, cles `KG-` | OK |
| Suite complete `go test -p 1 ./...` avec cette base, branche vs `4c27a4d` | memes 2 echecs preexistants des deux cotes (`TestCreateLicenseRefusesARetimedTrial`, `TestFulfillCheckout_UnmanagedSessionUsesTermsAtCheckout`), plus un test instable cote base (`TestQueueMarksPermanentFailureTerminal`) |

## 3. Informations RAPPORTEES, NON verifiees
- Identifiant du produit ALC dans l'instance Keygate de test : `afa82897-e92c-4cfe-9d9c-e04e198961b4` (fourni par l'equipe ; non relu dans l'admin).
- Le plan `ALC Perpetuelle` (1 activation) et une licence perpetuelle de test existent sur Blitz ; l'API publique de cle de signature repond.
- Les connexions SMTP classiques sont bloquees sur Blitz ; l'envoi d'e-mails n'est pas configure.
- Le domaine `alc-control.com` et `contact@alc-control.com` sont chez OVH ; une question de disponibilite regionale du VPS est en attente aupres du support OVH.

### Blitz (d'apres les captures de l'historique d'activite, 2026-10-11 ; aucun acces direct a Blitz)
- Une construction depuis `4c27a4d` apparait a 13:50 (statut "Replaced"), puis trois evenements "You started it" (14:00, 14:25, 14:42 "Live") qui
  affichent la MEME image `registry.blitz.cloud/u-keygate-alc/keygate@sha256:8d2ae19e...` : ce sont des redemarrages de la meme image.
- La ligne "4c27a4d" n'affiche pas son propre digest : le lien entre ce commit et l'image en ligne est PROBABLE mais pas formellement confirme.
- La page SETTINGS > Email du Keygate de test propose le fournisseur "Cloudflare Email" (HTTPS) ; l'envoi par SMTP depend des variables d'environnement
  (`SMTP_*`) et les ports SMTP sont bloques sur Blitz. Aucune configuration d'e-mail n'a ete enregistree par nous.

### Instance Blitz (lecture seule, API publique, 2026-10-11)
- `GET /api/v1/version` renvoie `version: dev`, `commit: unknown` (binaire construit sans ldflags) : le commit deploye ne peut PAS etre lu depuis l'API.
- `GET /health` : base de donnees `ok`.

### Blocages rencontres pour CDC-KEYGATE-02 (constates, non contournes)
- (leve) Poussee de la branche : le fork `ddbdjn488g-alt/Keygate-ALC` existe maintenant et la branche y est poussee.
- Blitz (variable `ALC_KEY_PRODUCT_IDS`, deploiement, journaux), console d'administration Keygate (creation de licence, reglages e-mail) et
  ALC Desktop : aucun acces pilotable depuis cet environnement (pas de session connectee, pas d'outil d'automatisation).

### Incident : cle `KG-` emise alors que `ALC_KEY_PRODUCT_IDS` est defini (2026-10-11, Blitz `keygate-alc-test`)
- Chemin de code verifie : `config.Load` -> `ALCKeyProductIDs` -> `main.go` (`db.KeyPolicy`) -> `NewAdminHandler(db, ...)` (meme pointeur) ->
  `AdminHandler.CreateLicense` -> `Store.NewLicenseKey(productID)` -> `KeyPolicy.Generate`. Le chemin est correct : le test d'integration
  `TestAdminCreateLicenseKeyFormatPerProduct` (PostgreSQL temporaire) produit bien une cle `ALC-` pour le produit configure.
- Cause reproduite dans le code : la lecture de la variable ne tolerait ni guillemets, ni espaces/retours a la ligne, ni BOM, ni autre separateur ;
  une valeur entouree de guillemets (frequente dans un tableau de bord ou un `.env`) ne correspondait plus exactement a l'identifiant et retombait
  silencieusement sur `KG-`. Test de reproduction : `TestALCKeyProductIDsTolerantParsing` (7 sous-cas en echec avant correction).
- Correction : `config.ParseProductIDs` (separateurs virgule / point-virgule / espaces ; guillemets, BOM et caracteres de largeur nulle retires ;
  comparaison ensuite EXACTE, pas de detection par le nom). Journaux ajoutes (sans cle) : au demarrage "license key format: ..." et a chaque
  emission "license key generated format=alc|kg product_id=...".
- NON etabli : que cette cause soit celle de Blitz (valeur reelle de la variable et commit construit non visibles depuis cet environnement).
  L'absence de la ligne de demarrage "license key format" dans les journaux Blitz prouverait que le code deploye n'est pas celui de la branche.

### Affichage : prefixe `KG-` sur des cles `ALC-` dans `/admin/licenses` (2026-10-11)
- Cause : l'interface codait en dur `KG-••••-` (`web/src/pages/admin/licenses.tsx`, `customers.tsx`) et l'API ne renvoyait que les 4 derniers
  caracteres (`license_key_hints`), sans aucune information de format. Donc un probleme de donnees renvoyees + de masque cote client, pas du CSS.
- Correction minimale : l'API renvoie en plus `license_key_prefixes` (liste, fiche client) et `license_key_prefix` (detail) = prefixe de la cle
  (`license.KeyPrefix`, texte avant le premier tiret, 1 a 8 majuscules/chiffres) ; l'interface l'utilise (`ALC-••••-XXXX` ou `KG-••••-XXXX`),
  avec `KG` en repli pour un serveur plus ancien. La cle complete n'est jamais renvoyee ; `license_key_hint(s)` est inchange.
- Tests : `TestKeyPrefix` (unitaire) et `TestAdminKeyMaskPrefixMatchesKeyFormat` (PostgreSQL temporaire, chiffrement au repos actif : liste,
  detail et fiche client, cles ALC et KG, aucune cle complete dans les reponses). Non verifie : rendu dans le navigateur et verification de types
  TypeScript (pas de `node_modules` dans cet environnement).

### Activation en ligne ALC <-> Keygate (2026-10-12)
- Constat : Keygate fournissait deja tout le serveur (`/license/activate`, `/verify`, `/pubkey`, jeton Ed25519, limite atomique, idempotence, liberation
  admin tracee). Ajouts cote Keygate : colonne "Activated" (date d'activation) dans le detail des licences, test d'integration
  `TestALCOnlineActivationFlow`, documentation `docs/alc-online-activation.md`.
- Cote ALC (branche `feature/alc-online-license`) : `Ed25519Verify.h`, `LicenseProofCore.h`, `LicenseOnline.h`, adaptation de `LicenseManager.h`,
  `LicenseScreen.h`, une ligne dans `MainWithScenes.h`, tests dans `Tools/license_tests/`.
- Verifie (execute) : voir le compte rendu de la mission ; NON verifie : Blitz (instance en veille, non joignable par programme), appareil physique,
  second Mac reel, parcours dans l'interface graphique.

### Echec du test reel sur le Mac (2026-10-12) : cause racine
- Le serveur Blitz etait bien a jour (`4493a7e`, journal "ALC format enabled") mais n'a recu AUCUNE requete d'activation : ALC n'a jamais lu `server.json`.
- Cause : sous macOS, JUCE place `userApplicationDataDirectory` dans `~/Library` (et non `~/Library/Application Support`). ALC cherchait donc
  `~/Library/ALC_License/server.json` ; le fichier etait dans `~/Library/Application Support/ALC_License/`. Configuration absente -> activation locale historique.
- Correction (branche ALC) : les deux dossiers sont lus ; configuration presente mais inutilisable -> erreur explicite, pas de repli local ;
  journal `[ALC licence]` sans secret ; messages de renouvellement explicites. Verifie avec le vrai `server.json` de l'utilisateur (lecture seule) et en bout en bout
  contre un serveur Keygate local.

## 4. Decisions prises
- Le format historique d'ALC fait reference ; Keygate s'adapte, ALC n'est pas modifie.
- Format ALC limite aux produits listes dans `ALC_KEY_PRODUCT_IDS` (identifiants reels, pas de deduction par le nom).
- Collisions : verification du hash + nouvelle tentative bornee ; l'index unique reste la garantie finale.
- Aucun deploiement, aucun push de branche de deploiement sans autorisation explicite.
- Blitz = environnement temporaire de validation ; la production visee est un VPS OVH (France / Europe).

## 5. Hypotheses a confirmer
- Le commit `4c27a4d` correspond a la version actuellement deployee sur Blitz (NON verifie : aucun acces a Blitz).
- Le comportement de Keygate avec un volume reel d'activations et de renouvellements n'est pas evalue.
- Le jeton signe actuel de Keygate suffira a la verification locale dans ALC (a analyser dans un CDC distinct).

## 6. Limites de securite connues (format ALC)
- ~40 bits aleatoires (8 caracteres) au lieu de 160.
- Le checksum est public dans le binaire d'ALC : une cle valide localement peut etre fabriquee sans Keygate.
- ALC ne verifie pas la cle aupres de Keygate : revocation, suspension, expiration et compteur d'activations ne sont pas vus par l'application.
- Cette modification rend les cles ACCEPTEES par le validateur actuel ; ce n'est pas un systeme de licences complet.

## 7. Taches en attente (ordre de priorite)
1. Priorite 1 (action humaine : aucun acces a Blitz depuis cet environnement) : dans Blitz, pointer le deploiement sur le fork
   `ddbdjn488g-alt/Keygate-ALC`, branche `feature/alc-license-key-format` ; definir `ALC_KEY_PRODUCT_IDS=afa82897-e92c-4cfe-9d9c-e04e198961b4` ;
   redeployer ; creer une NOUVELLE licence de test pour ALC ; verifier le prefixe `ALC-` ; tester la cle dans ALC Desktop
   (preuve limitee : format + checksum acceptes localement). Ne pas toucher a la licence `KG-` existante.
2. Priorite 2 : rejouer le test d'integration PostgreSQL dans une base de test dediee de l'equipe (le test fourni cree ses propres lignes).
3. Priorite 3 : relier ALC a Keygate (activation en ligne, jeton signe, verification locale) : analyse puis CDC distinct.
4. Priorite 4 : tester limite d'une machine, refus d'une 2e activation, transfert controle, persistance hors ligne, revocation / reactivation ; documenter les limites.
5. Priorite 5 : preparer la production OVH (apres validation en test et confirmation de la disponibilite du VPS).
6. Portabilite : eviter les URL codees en dur (Blitz et lien Stripe de test dans ALC) ; adresse du serveur, identifiants de produit et secrets configurables par environnement.

## 8. Interdictions / elements a ne pas modifier
- ALC : audio, MIDI, communications avec les processeurs, regles musicales, identifiants materiels, ecran d'activation, validateur de cle (sans autorisation explicite).
- La licence perpetuelle de test existante (ne pas la supprimer, la modifier ni consommer son unique activation).
- DNS, OVH, messagerie, secrets et variables de production.
- Ne rien pousser sur une branche de deploiement ni deployer sur Blitz sans autorisation explicite.
- Ne jamais ecrire de cle de licence, de jeton ou de secret dans un rapport ou un journal.
- Ne pas installer de logiciel global : Go est temporaire dans `/tmp/alc-go`, PostgreSQL temporaire dans `/tmp/alc-pg` (arrete).
