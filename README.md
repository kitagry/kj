## kj

Create and Edit Kubernetes job from cronjob template using your `$EDITOR`.

![kj](https://user-images.githubusercontent.com/21323222/121770891-0e033400-cba7-11eb-9d99-4cdc473f5774.gif)

### Usage

```
kj namespace name
kj namespace/name
kj name
```

This command opens the editor with the job yaml from specified cronjob.
`kj` command apply your changes.

### Install

#### build from source

Requirement: Go 1.16+

```
go install github.com/kitagry/kj@latest
```

#### MacOS

```
brew install kitagry/tap/kj
```

### Expansion

This command is simple CLI, and expansiable.
For example, you can write following code in your `.zshrc`, and then use `kjf` command.
Watch the video for the acutual behavior.
You can use your favorite fuzzy finder(fzf, peco, etc).

```zsh
kjf() {
  kubectl get cronjobs --all-namespaces | tr -s ' ' | cut -d ' ' -f 1,2 | tail -n +2 | fzf | xargs kj
}
```

### Inspired

- [mmv](https://github.com/itchyny/mmv)
