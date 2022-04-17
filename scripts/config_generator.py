import argparse

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='generate experiment config under ../experiments')
    parser.add_argument('-c', '--cluster', type=str, default="paib", help='cluster config' )
    parser.add_argument('-p', '--pod', type=str, default="paib", help='pod config')
    
    parser.add_argument('-f', '--defrag', type=int, default=1000, help='defragment score')
    parser.add_argument('-p', '--pack', type=int, default=0, help='packing score')
    parser.add_argument('-b', '--balance', type=int, default=0, help='load-balancing score')
    
    parser.add_argument('-i', '--inflation', type=int, default=150, help='inflation percentage')
    parser.add_argument('-d', '--deschedule', type=int, default=10, help='deschedule percentage')

    parser.add_argument('--cpu_involve', dest='cpu_involve', action='store_true', help="Typical pods: involve CPU pods")
    parser.set_defaults(cpu_involve=False)
    parser.add_argument('-ppt', '--pod_pop_thres', type=int, default=95, help='Typical pods: pod popularity threshold')
    parser.add_argument('-gpu', '--gpu_weight', type=int, default=0, help='Typical pods: gpu weight')

    parser.add_argument('--gpu', dest='gpu', action='store_true', help='Export GPU only scenario')

    args = parser.parse_args()

