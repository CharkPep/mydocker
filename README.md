## MyDocker challenge done on [CodeCrafters](https://app.codecrafters.io/catalog)

### Build
```go
go build -o mydocker .
```

### Run
```bash
./mydocker run image:tag path_to_proccess ...args
```

### Example
```bash
./mydocker run ubuntu:latest /bin/echo hello
```