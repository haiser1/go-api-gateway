from flask import Flask, jsonify

app = Flask(__name__)


@app.route("/api/v1/hello-server-1")
def hello():
    return jsonify({"message": "Hello, World! from server 1"})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5002, debug=True)
