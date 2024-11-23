#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <sys/socket.h>
#include <fcntl.h>
#include <stdbool.h>
#include <errno.h>
#include <netinet/tcp.h>

#define BROADCAST_PORT 8000
#define BROADCAST_ADDR "192.168.100.255"
#define TCP_PORT 8001
#define BUFFER_SIZE 1024
#define TIMEOUT_BROADCAST_SEC 2
#define TIMEOUT_TCP_USEC 1000
#define MAX_TASKS 10
#define MAX_SERVERS 100

typedef struct Task {
    int id;
    double l;
    double r;
    double res;
    bool done;
} Task;

typedef struct Server {
    struct sockaddr_in addr;
    int socket;
    Task* tasks[MAX_TASKS];
    int n_tasks;
    bool is_good;
} Server;

Server servers[MAX_SERVERS];
Task* free_tasks[MAX_SERVERS];
int n_free_tasks = 0;

void set_tcp_keepalive(int sockfd, int keep_idle, int keep_interval, int keep_count) {
    int optval = 1;

    // Включить TCP Keepalive
    if (setsockopt(sockfd, SOL_SOCKET, SO_KEEPALIVE, &optval, sizeof(optval)) < 0) {
        perror("setsockopt(SO_KEEPALIVE) failed");
        exit(EXIT_FAILURE);
    }

    // Установить временной промежуток, через который будут начинаться Keepalive пакеты
    if (setsockopt(sockfd, IPPROTO_TCP, TCP_KEEPIDLE, &keep_idle, sizeof(keep_idle)) < 0) {
        perror("setsockopt(TCP_KEEPIDLE) failed");
        exit(EXIT_FAILURE);
    }

    // Установить интервал между Keepalive пакетами
    if (setsockopt(sockfd, IPPROTO_TCP, TCP_KEEPINTVL, &keep_interval, sizeof(keep_interval)) < 0) {
        perror("setsockopt(TCP_KEEPINTVL) failed");
        exit(EXIT_FAILURE);
    }

    // Установить количество Keepalive пакетов до закрытия соединения
    if (setsockopt(sockfd, IPPROTO_TCP, TCP_KEEPCNT, &keep_count, sizeof(keep_count)) < 0) {
        perror("setsockopt(TCP_KEEPCNT) failed");
        exit(EXIT_FAILURE);
    }
}

void send_broadcast(int udp_sock) {
    struct sockaddr_in broadcast_addr;
    char *message = "DISCOVER";

    memset(&broadcast_addr, 0, sizeof(broadcast_addr));
    broadcast_addr.sin_family = AF_INET;
    broadcast_addr.sin_port = htons(BROADCAST_PORT);
    inet_pton(AF_INET, BROADCAST_ADDR, &broadcast_addr.sin_addr);

    int broadcast_enable = 1;
    setsockopt(udp_sock, SOL_SOCKET, SO_BROADCAST, &broadcast_enable, sizeof(broadcast_enable));

    if (sendto(udp_sock, message, strlen(message), 0, 
               (struct sockaddr*)&broadcast_addr, sizeof(broadcast_addr)) < 0) {
        perror("sendto");
        exit(EXIT_FAILURE);
    }
}

void set_socket_timeout(int sock, int sec, int usec) {
    struct timeval timeout;
    timeout.tv_sec = sec;
    timeout.tv_usec = usec;
    if (setsockopt(sock, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) < 0) {
        perror("setsockopt(SO_RCVTIMEO) failed");
        exit(EXIT_FAILURE);
    }
}

bool try_connect(int server_id) {
    int tcp_sock = socket(AF_INET, SOCK_STREAM, 0);
    if (tcp_sock < 0) {
        return false;
    }

    fd_set write_fds;
    struct timeval tv;

    int flags = fcntl(tcp_sock, F_GETFL, 0);
    fcntl(tcp_sock, F_SETFL, flags | O_NONBLOCK);

    if (connect(tcp_sock, (struct sockaddr*)&servers[server_id].addr, sizeof(servers[server_id].addr)) < 0) {
        if (errno == EINPROGRESS) {
            FD_ZERO(&write_fds);
            FD_SET(tcp_sock, &write_fds);

            tv.tv_sec = 2;
            tv.tv_usec = 0;

            int result = select(tcp_sock + 1, NULL, &write_fds, NULL, &tv);
            if (result > 0) {
                int so_error;
                socklen_t len = sizeof(so_error);
                getsockopt(tcp_sock, SOL_SOCKET, SO_ERROR, &so_error, &len);
                if (so_error == 0) {
                    fcntl(tcp_sock, F_SETFL, flags);
                    set_tcp_keepalive(tcp_sock, 2, 1, 3);
                    servers[server_id].socket = tcp_sock;
                    servers[server_id].is_good = true;
                    printf("Server %d connected\n", server_id);
                    return true;
                } else {
                    return false;
                }
            } else if (result == 0) {
                return false;
            } else {
                return false;
            }
        } else {
            return false;
        }
    }

    fcntl(tcp_sock, F_SETFL, flags);
    set_tcp_keepalive(tcp_sock, 2, 1, 3);
    servers[server_id].socket = tcp_sock;
    servers[server_id].is_good = true;
    printf("Server %d connected\n", server_id);
    return true;
}

int handle_broadcast_responses(int udp_sock) {
    struct sockaddr_in server_addr;
    socklen_t addr_len = sizeof(server_addr);
    char buffer[BUFFER_SIZE];

    // Устанавливаем тайм-аут на 2 секунды для получения ответов
    set_socket_timeout(udp_sock, TIMEOUT_BROADCAST_SEC, 0);

    int i = 0;
    for (;; ++i) {
        int recv_len = recvfrom(udp_sock, buffer, BUFFER_SIZE - 1, 0,
                                (struct sockaddr*)&server_addr, &addr_len);
        if (recv_len <= 0) {
            break; // Заканчиваем при первом возникновении тайм-аута
        }

        buffer[recv_len] = '\0';
        server_addr.sin_port = htons(TCP_PORT);
        servers[i].addr = server_addr;
        servers[i].n_tasks = 0;

        try_connect(i);
    }
    return i;
}

void server_break(int server_id) {
    printf("Server %d disconnected\n", server_id);
    close(servers[server_id].socket);
    servers[server_id].is_good = false;
    for (int j = 0; j < servers[server_id].n_tasks; ++j) {
        if (servers[server_id].tasks[j] != NULL) {
            free_tasks[n_free_tasks++] = servers[server_id].tasks[j];
            servers[server_id].tasks[j] = NULL;
        }
    }
    servers[server_id].n_tasks = 0;
}

void send_task(int server_id, Task* task) {
    servers[server_id].tasks[servers[server_id].n_tasks++] = task;
    char buffer[BUFFER_SIZE];
    sprintf(buffer, "%d %lf %lf", task->id, task->l, task->r);

    if (send(servers[server_id].socket, buffer, strlen(buffer), 0) < 0) {
        server_break(server_id);
    }
}

int main(int argc, char* argv[]) {
    if (argc != 3) {
        printf("Bad argc\n");
        return 1;
    }
    int udp_sock = socket(AF_INET, SOCK_DGRAM, 0);
    if (udp_sock < 0) {
        perror("socket");
        exit(EXIT_FAILURE);
    }

    send_broadcast(udp_sock);
    int n_servers = handle_broadcast_responses(udp_sock);

    close(udp_sock);

    double l = atof(argv[1]);
    double r = atof(argv[2]);
    double delta = (r - l) / n_servers;

    Task tasks[MAX_SERVERS];

    for (int i = 0; i < n_servers; ++i) {
        double tl = l + delta * i;
        double tr = l + delta * (i + 1);

        tasks[i].id = i;
        tasks[i].l = tl;
        tasks[i].r = tr;
        tasks[i].done = false;
        
        send_task(i, &tasks[i]);
    }

    char buffer[BUFFER_SIZE];

    int completed_tasks = 0;
    while (completed_tasks < n_servers) {
        for (int i = 0; i < n_servers; ++i) {
            if (!servers[i].is_good) {
                try_connect(i);
            }
        }

        for (int i = 0; i < n_servers; ++i) {
            if (!servers[i].is_good) {
                continue;
            }

            fd_set read_fds;
            FD_ZERO(&read_fds);
            FD_SET(servers[i].socket, &read_fds);

            struct timeval timeout;
            timeout.tv_sec = 0;
            timeout.tv_usec = 100;

            int activity = select(servers[i].socket + 1, &read_fds, NULL, NULL, &timeout);
            if (activity < 0) {
                perror("select");
                break;
            } else if (activity == 0 || !FD_ISSET(servers[i].socket, &read_fds)) {
                continue;
            }

            int recv_len = recv(servers[i].socket, buffer, BUFFER_SIZE - 1, 0);
            if (recv_len < 0) {
                server_break(i);
            } else if (recv_len == 0) {
                server_break(i);
            } else {
                buffer[recv_len] = '\0';
                int task_id;
                double task_res;
                sscanf(buffer, "%d %lf", &task_id, &task_res);
                if (!tasks[task_id].done) {
                    tasks[task_id].res = task_res;
                    tasks[task_id].done = true;
                    ++completed_tasks;
                }
                for (int j = 0; j < MAX_TASKS; ++j) {
                    if (servers[i].tasks[j] != NULL && servers[i].tasks[j]->id == task_id) {
                        for (int k = j; k < MAX_TASKS - 1; ++k) {
                            servers[i].tasks[k] = servers[i].tasks[k + 1];
                        }
                        servers[i].tasks[MAX_TASKS - 1] = NULL;
                        --servers[i].n_tasks;
                    }
                }
            }
        }

        int chosen_serv = 0;
        for (int i = 0; i < n_free_tasks; ++i) {
            int tried = 0;
            while (tried < n_servers && !servers[chosen_serv].is_good) {
                chosen_serv = (chosen_serv + 1) % n_servers;
                ++tried;
            }
            if (tried == n_servers) {
                break;
            }

            send_task(chosen_serv, free_tasks[i]);
            chosen_serv = (chosen_serv + 1) % n_servers;
            free_tasks[i] = NULL;
        }
        n_free_tasks = 0;
    }

    double res = 0;

    for (int i = 0; i < n_servers; ++i) {
        close(servers[i].socket);
        res += tasks[i].res;
    }
    printf("%lf\n", res);
    return 0;
}