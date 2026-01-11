from flask import Flask, jsonify, request

app = Flask(__name__)


@app.route("/api/v1/hello-server-2")
def hello():
    return jsonify({"messagge": "Hello, World! from server 2"})


@app.route("/api/v1/hello/<name>")
def hello_name(name):
    last_name = request.args.get("last_name")
    return jsonify({"message": f"Hello, {name} {last_name}!"})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5001, debug=True)
