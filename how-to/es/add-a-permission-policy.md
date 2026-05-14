# Cómo añadir una política de permisos

Objetivo: reemplazar "preguntar cada vez" por algo más inteligente — auto-permitir `read_file`, preguntar una vez por comando de shell, denegar escrituras fuera del directorio de trabajo.

El harness viene con una sola estrategia: `Agent.Confirm` se llama en cada tool call, y la respuesta es sí/no. Todavía no hay abstracción. La primera vez que quieres cualquier otra cosa, introduces una interfaz `PermissionPolicy` y haces que el agente pase por ella. Esta guía cubre las dos cosas: el trabajo único de introducir la abstracción, y después la receta para añadir políticas nuevas.

## Parte 1 — una sola vez: introducir la abstracción

### 1. Añade la interfaz de política

Crea `internal/permission/policy.go`:

```go
// Package permission decide si una tool call debe ejecutarse, preguntar o ser
// denegada. El bucle del agente consulta una Policy antes de cada tool call.
package permission

import "context"

type Decision int

const (
	DecisionAsk Decision = iota // rutar a través de Agent.Confirm
	DecisionAllow                // ejecutar sin preguntar
	DecisionDeny                 // rechazar; la razón se le muestra al modelo
)

type Policy interface {
	Decide(ctx context.Context, name, input string) (Decision, string)
}
```

El segundo valor de retorno es una cadena con la razón. Para `DecisionDeny`, se convierte en el tool result que ve el modelo (para que pueda autocorregirse). Para `DecisionAllow` y `DecisionAsk`, se ignora.

### 2. Refactoriza `Agent.executeTool`

En [`internal/agent/agent.go`](../../internal/agent/agent.go), añade un campo `Policy`:

```go
type Agent struct {
	// … campos existentes …
	Policy   permission.Policy
}
```

Y cambia `executeTool` para que la consulte:

```go
func (a *Agent) executeTool(ctx context.Context, name, rawInput string) (string, bool) {
	fmt.Printf("%s[tool] %s %s\n", a.LogPrefix, name, rawInput)

	decision := permission.DecisionAsk
	reason := ""
	if a.Policy != nil {
		decision, reason = a.Policy.Decide(ctx, name, rawInput)
	}

	switch decision {
	case permission.DecisionAllow:
		// continuar
	case permission.DecisionDeny:
		if reason == "" {
			reason = "permission policy denied this tool call"
		}
		return reason, true
	case permission.DecisionAsk:
		if a.Confirm != nil && !a.Confirm("approve?") {
			return "user denied this tool call", true
		}
	}
	return a.Tools.Execute(ctx, name, rawInput)
}
```

Un `Policy` nulo cae al comportamiento antiguo (preguntar siempre). Compatibilidad hacia atrás.

### 3. Conecta una política por defecto en `main.go`

```go
import "github.com/betta-tech/byo-coding-agent/internal/permission"

// En main(), después de construir rootAgent:
rootAgent.Policy = permission.AlwaysAsk{}
```

Listo con la abstracción. Compila, ejecuta — el comportamiento no cambia porque `AlwaysAsk` devuelve `DecisionAsk` para todo, que es exactamente lo que hacía el código antiguo.

## Parte 2 — repetible: añadir una política nueva

Después de la Parte 1, añadir una política es un archivo nuevo. Cada política es una struct que implementa `permission.Policy`.

### Ejemplo: `AlwaysAllow`

```go
// internal/permission/always_allow.go
package permission

import "context"

type AlwaysAllow struct{}

func (AlwaysAllow) Decide(_ context.Context, _, _ string) (Decision, string) {
	return DecisionAllow, ""
}
```

### Ejemplo: `AllowList`

Auto-permite herramientas nombradas, pregunta por todo lo demás:

```go
// internal/permission/allow_list.go
package permission

import "context"

type AllowList struct {
	Names map[string]bool
}

func (a AllowList) Decide(_ context.Context, name, _ string) (Decision, string) {
	if a.Names[name] {
		return DecisionAllow, ""
	}
	return DecisionAsk, ""
}
```

Conéctala:

```go
rootAgent.Policy = permission.AllowList{
	Names: map[string]bool{
		"read_file":              true,
		"deepwiki_ask_question":  true,
	},
}
```

### Ejemplo: `AskOnce`

Pregunta la primera vez por cada nombre de herramienta, recuerda la respuesta para la sesión:

```go
// internal/permission/ask_once.go
package permission

import (
	"context"
	"sync"
)

type AskOnce struct {
	mu      sync.Mutex
	allowed map[string]bool
}

func (a *AskOnce) Decide(_ context.Context, name, _ string) (Decision, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allowed == nil {
		a.allowed = map[string]bool{}
	}
	if a.allowed[name] {
		return DecisionAllow, ""
	}
	// Primera llamada: pregunta, y deja que quien llama recuerde la respuesta.
	// No podemos mutar desde dentro de Decide porque todavía no conocemos
	// la respuesta — mira "Tropiezos" abajo para ver cómo manejarlo.
	return DecisionAsk, ""
}

// AfterAsk lo llama el bucle del agente después de un Confirm exitoso para
// registrar el "sí" del usuario y no volver a preguntar por esta herramienta.
func (a *AskOnce) AfterAsk(name string, allowed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allowed == nil {
		a.allowed = map[string]bool{}
	}
	a.allowed[name] = allowed
}
```

Esta necesita un pequeño cambio en `executeTool` — después de que `Confirm` retorne, llamar a `policy.AfterAsk` si la política lo soporta (type-assertion, mismo idiom que `tokenReporter` en `commands.go`). Vale la pena hacerlo una vez, y después cada política estilo `AskOnce` reutiliza el hook.

### Ejemplo: `PathScoped`

Permite `read_file` / `write_file` solo si su path se mantiene bajo el CWD:

```go
// internal/permission/path_scoped.go
package permission

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type PathScoped struct {
	// Inner se consulta para todo lo que no sea una herramienta con forma de path.
	Inner Policy
}

func (p PathScoped) Decide(ctx context.Context, name, input string) (Decision, string) {
	switch name {
	case "read_file", "write_file":
		var in struct{ Path string `json:"path"` }
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return DecisionDeny, "invalid tool input"
		}
		cwd, _ := os.Getwd()
		abs, err := filepath.Abs(in.Path)
		if err != nil || !strings.HasPrefix(abs, cwd+string(filepath.Separator)) {
			return DecisionDeny, "path is outside the working directory"
		}
		return DecisionAllow, ""
	}
	if p.Inner != nil {
		return p.Inner.Decide(ctx, name, input)
	}
	return DecisionAsk, ""
}
```

Compone con otras políticas: `PathScoped{Inner: AllowList{...}}` aplica el límite del path *y* delega en una allow-list para todo lo demás.

## Convenciones

- **Las políticas son sin estado, salvo que necesiten memoria de sesión.** La mayoría son structs simples con campos de configuración y sin goroutines.
- **Las políticas con estado necesitan un mutex.** `AskOnce` es el ejemplo — los subagentes pueden ejecutarse en goroutines que tocan la misma política.
- **Composición antes que enum.** Construye políticas complejas envolviendo políticas más simples (`PathScoped{Inner: AskOnce{Inner: AllowList{...}}}`), no añadiendo casos a un switch dentro de `Decide`. Más limpio para extender.
- **Las razones son para el modelo.** Cuando devuelves `DecisionDeny`, la cadena de la razón se le pasa al modelo como un tool result con `is_error: true`. Escríbela como si le contaras al modelo qué pasó: "path is outside the working directory" funciona; "ENOENT" no.

## Tropiezos

- **No hagas la política interactiva.** `Decide` debería ser síncrono e instantáneo. Si necesitas un prompt, devuelve `DecisionAsk` y deja que el flujo existente de `Confirm` lo maneje.
- **Pruébalo con subagentes.** Los subagentes (capítulo 11) usan la misma política que el agente root por defecto. Una política que pregunta por shell va a bloquear a un subagente que se supone debe ejecutarse sin supervisión. Decide si los subagentes reciben una política distinta en el momento del registro.
- **`Decide` se ejecuta dentro del bucle del agente**, que tiene el contexto de la conversación. No bloquees con llamadas de red, no llames al modelo desde dentro de `Decide`. Cualquier función de "permiso juzgado por IA" necesita un diseño distinto.

## Ver también

- [`follow_along/es/02-the-permission-gate.md`](../../follow_along/es/02-the-permission-gate.md) para la línea base de "preguntar cada vez" y la justificación de diseño de poner la aprobación en la capa del harness.
